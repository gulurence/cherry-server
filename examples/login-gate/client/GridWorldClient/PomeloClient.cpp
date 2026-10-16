#include "pch.h"
#include "PomeloClient.h"

#include <chrono>

#pragma comment(lib, "ws2_32.lib")

// pomelo 包类型（net/parser/pomelo/packet/const.go）
static const uint8_t PKT_HANDSHAKE = 0x01;
static const uint8_t PKT_HANDSHAKE_ACK = 0x02;
static const uint8_t PKT_HEARTBEAT = 0x03;
static const uint8_t PKT_DATA = 0x04;
static const uint8_t PKT_KICK = 0x05;

// pomelo 消息类型（net/parser/pomelo/message/const.go）
static const uint8_t MSG_REQUEST = 0x00;
static const uint8_t MSG_NOTIFY = 0x01;
static const uint8_t MSG_RESPONSE = 0x02;
static const uint8_t MSG_PUSH = 0x03;

static const uint8_t FLAG_COMPRESS_ROUTE = 0x01;
static const uint8_t FLAG_ERROR = 0x20;

static const uint64_t HEARTBEAT_INTERVAL_MS = 30000;

uint64_t nowMs() {
    using namespace std::chrono;
    return static_cast<uint64_t>(
        duration_cast<milliseconds>(steady_clock::now().time_since_epoch()).count());
}

// ---- 消息编解码（net/parser/pomelo/message/message.go 的 C++ 等价实现） ----

namespace {

// varint（128 变长，低位在前）编码
void writeVarint(std::vector<uint8_t>& out, uint32_t v) {
    while (v >= 0x80) {
        out.push_back(static_cast<uint8_t>(v & 0x7F) | 0x80);
        v >>= 7;
    }
    out.push_back(static_cast<uint8_t>(v));
}

bool readVarint(const std::vector<uint8_t>& b, size_t& pos, uint32_t& out) {
    out = 0;
    int shift = 0;
    while (pos < b.size()) {
        uint8_t c = b[pos++];
        out |= static_cast<uint32_t>(c & 0x7F) << shift;
        if (!(c & 0x80)) return true;
        shift += 7;
        if (shift > 28) return false;
    }
    return false;
}

struct DecodedMsg {
    uint8_t type = 0;
    uint32_t id = 0;
    std::string route;
    std::string data;
    bool error = false;
};

bool decodeMessage(const std::vector<uint8_t>& body, DecodedMsg& m) {
    if (body.empty()) return false;
    size_t pos = 0;
    uint8_t flag = body[pos++];
    m.type = (flag >> 1) & 0x07;
    m.error = (flag & FLAG_ERROR) != 0;
    m.id = 0;

    if (m.type == MSG_REQUEST || m.type == MSG_RESPONSE) {
        if (!readVarint(body, pos, m.id)) return false;
    }

    if (m.type == MSG_REQUEST || m.type == MSG_NOTIFY || m.type == MSG_PUSH) {
        if (flag & FLAG_COMPRESS_ROUTE) {
            // 路由压缩：2 字节大端字典码。服务器默认字典为空，不会用到；忽略。
            if (pos + 2 > body.size()) return false;
            pos += 2;
            m.route.clear();
        } else {
            if (pos >= body.size()) return false;
            size_t len = body[pos++];
            if (pos + len > body.size()) return false;
            m.route.assign(reinterpret_cast<const char*>(&body[pos]), len);
            pos += len;
        }
    }

    m.data.assign(reinterpret_cast<const char*>(&body[pos]), body.size() - pos);
    return true;
}

} // namespace

PomeloClient::PomeloClient() = default;

PomeloClient::~PomeloClient() {
    Close();
}

bool PomeloClient::Connect(const std::string& host, uint16_t port) {
    Close();

    sock_ = socket(AF_INET, SOCK_STREAM, IPPROTO_TCP);
    if (sock_ == INVALID_SOCKET) return false;

    sockaddr_in addr{};
    addr.sin_family = AF_INET;
    addr.sin_port = htons(port);
    if (inet_pton(AF_INET, host.c_str(), &addr.sin_addr) != 1) {
        closesocket(sock_);
        sock_ = INVALID_SOCKET;
        return false;
    }

    // 连接超时：设置非阻塞 connect + select。
    u_long nb = 1;
    ioctlsocket(sock_, FIONBIO, &nb);
    int rc = connect(sock_, reinterpret_cast<sockaddr*>(&addr), sizeof(addr));
    if (rc == SOCKET_ERROR) {
        int err = WSAGetLastError();
        if (err == WSAEWOULDBLOCK || err == WSAEINPROGRESS) {
            fd_set wf;
            FD_ZERO(&wf);
            FD_SET(sock_, &wf);
            timeval tv{5, 0};
            rc = select(0, nullptr, &wf, nullptr, &tv);
            if (rc <= 0) {
                closesocket(sock_);
                sock_ = INVALID_SOCKET;
                return false;
            }
            int soerr = 0;
            int len = sizeof(soerr);
            getsockopt(sock_, SOL_SOCKET, SO_ERROR, reinterpret_cast<char*>(&soerr), &len);
            if (soerr != 0) {
                closesocket(sock_);
                sock_ = INVALID_SOCKET;
                return false;
            }
        } else {
            closesocket(sock_);
            sock_ = INVALID_SOCKET;
            return false;
        }
    }
    nb = 0;
    ioctlsocket(sock_, FIONBIO, &nb);

    connected_ = true;

    // 握手：发 Handshake -> 读服务器 Handshake 响应 -> 回 HandshakeAck。
    sendPacket(PKT_HANDSHAKE, nullptr, 0);
    uint8_t type = 0;
    std::vector<uint8_t> body;
    if (!waitReadable(5000) || !readPacketNoWait(type, body) || type != PKT_HANDSHAKE) {
        Close();
        return false;
    }
    sendPacket(PKT_HANDSHAKE_ACK, nullptr, 0);

    lastHeartbeatMs_ = nowMs();
    running_ = true;
    recvThread_ = std::thread([this] { recvLoop(); });
    return true;
}

void PomeloClient::Close() {
    if (running_.load()) {
        running_ = false;
        if (sock_ != INVALID_SOCKET) {
            closesocket(sock_);
            sock_ = INVALID_SOCKET;
        }
    }
    if (recvThread_.joinable()) {
        recvThread_.join();
    }
    if (sock_ != INVALID_SOCKET) {
        closesocket(sock_);
        sock_ = INVALID_SOCKET;
    }
    connected_ = false;
}

uint32_t PomeloClient::Request(const std::string& route, const std::string& jsonBody) {
    uint32_t id = nextId_.fetch_add(1);
    std::vector<uint8_t> msg;
    msg.push_back(MSG_REQUEST << 1); // flag
    writeVarint(msg, id);
    msg.push_back(static_cast<uint8_t>(route.size()));
    msg.insert(msg.end(), route.begin(), route.end());
    msg.insert(msg.end(), jsonBody.begin(), jsonBody.end());
    sendPacket(PKT_DATA, msg.data(), msg.size());
    return id;
}

void PomeloClient::Notify(const std::string& route, const std::string& jsonBody) {
    std::vector<uint8_t> msg;
    msg.push_back(MSG_NOTIFY << 1); // flag
    msg.push_back(static_cast<uint8_t>(route.size()));
    msg.insert(msg.end(), route.begin(), route.end());
    msg.insert(msg.end(), jsonBody.begin(), jsonBody.end());
    sendPacket(PKT_DATA, msg.data(), msg.size());
}

void PomeloClient::recvLoop() {
    while (running_.load()) {
        if (!waitReadable(1000)) {
            maybeSendHeartbeat();
            continue;
        }
        uint8_t type = 0;
        std::vector<uint8_t> body;
        if (!readPacketNoWait(type, body)) break;
        processPacket(type, body);
        maybeSendHeartbeat();
    }
    running_ = false;
    if (notifyWnd_) {
        ::PostMessage(notifyWnd_, WM_APP_CLOSED, 0, 0);
    }
}

bool PomeloClient::waitReadable(int timeoutMs) {
    fd_set rf;
    FD_ZERO(&rf);
    FD_SET(sock_, &rf);
    timeval tv{timeoutMs / 1000, (timeoutMs % 1000) * 1000};
    int r = select(0, &rf, nullptr, nullptr, &tv);
    return r > 0;
}

bool PomeloClient::readExact(char* buf, int len) {
    int got = 0;
    while (got < len) {
        int n = recv(sock_, buf + got, len - got, 0);
        if (n == 0) return false; // 对端关闭
        if (n == SOCKET_ERROR) {
            int e = WSAGetLastError();
            if (e == WSAEINTR) continue;
            return false;
        }
        got += n;
    }
    return true;
}

bool PomeloClient::readPacketNoWait(uint8_t& type, std::vector<uint8_t>& body) {
    char hdr[4];
    if (!readExact(hdr, 4)) return false;
    type = static_cast<uint8_t>(hdr[0]);
    uint32_t len = (static_cast<uint8_t>(hdr[1]) << 16) |
                   (static_cast<uint8_t>(hdr[2]) << 8) |
                   static_cast<uint8_t>(hdr[3]);
    body.resize(len);
    if (len > 0 && !readExact(reinterpret_cast<char*>(body.data()), static_cast<int>(len))) {
        return false;
    }
    return true;
}

void PomeloClient::sendAll(const uint8_t* data, size_t len) {
    size_t sent = 0;
    while (sent < len) {
        int n = send(sock_, reinterpret_cast<const char*>(data + sent),
                     static_cast<int>(len - sent), 0);
        if (n == SOCKET_ERROR) {
            int e = WSAGetLastError();
            if (e == WSAEINTR) continue;
            break;
        }
        sent += static_cast<size_t>(n);
    }
}

void PomeloClient::sendPacket(uint8_t type, const uint8_t* body, size_t len) {
    if (sock_ == INVALID_SOCKET) return;
    uint8_t hdr[4] = {
        type,
        static_cast<uint8_t>((len >> 16) & 0xFF),
        static_cast<uint8_t>((len >> 8) & 0xFF),
        static_cast<uint8_t>(len & 0xFF),
    };
    sendAll(hdr, 4);
    if (len > 0) sendAll(body, len);
}

void PomeloClient::maybeSendHeartbeat() {
    uint64_t now = nowMs();
    if (now - lastHeartbeatMs_.load() >= HEARTBEAT_INTERVAL_MS) {
        lastHeartbeatMs_ = now;
        sendPacket(PKT_HEARTBEAT, nullptr, 0);
    }
}

void PomeloClient::processPacket(uint8_t type, const std::vector<uint8_t>& body) {
    if (type == PKT_HEARTBEAT) return; // 服务器对心跳的回应，忽略
    if (type == PKT_KICK) {
        if (notifyWnd_) ::PostMessage(notifyWnd_, WM_APP_CLOSED, 0, 0);
        return;
    }
    if (type != PKT_DATA) return;

    DecodedMsg m;
    if (!decodeMessage(body, m)) return;

    if (m.type == MSG_RESPONSE) {
        auto* p = new ResponsePayload{m.error, m.data};
        if (notifyWnd_) {
            ::PostMessage(notifyWnd_, WM_APP_RESPONSE, static_cast<WPARAM>(m.id),
                          reinterpret_cast<LPARAM>(p));
        } else {
            delete p;
        }
    } else if (m.type == MSG_PUSH) {
        auto* p = new PushPayload{m.route, m.data};
        if (notifyWnd_) {
            ::PostMessage(notifyWnd_, WM_APP_PUSH, 0, reinterpret_cast<LPARAM>(p));
        } else {
            delete p;
        }
    }
}
