#pragma once

#include <atomic>
#include <cstdint>
#include <string>
#include <thread>
#include <vector>

// 自定义窗口消息：后台接收线程把结果投递到 UI 线程（HWND）。
#define WM_APP_RESPONSE (WM_APP + 1) // wParam = msgId, lParam = (ResponsePayload*)
#define WM_APP_PUSH     (WM_APP + 2) // wParam = 0, lParam = (PushPayload*)
#define WM_APP_CLOSED   (WM_APP + 3) // 连接被对端关闭 / 出错

struct ResponsePayload {
    bool isError = false;
    std::string json;
};

struct PushPayload {
    std::string route;
    std::string json;
};

// PomeloClient 封装 pomelo 协议（握手 / 心跳 / 请求 / 推送）与底层 TCP socket。
//
// 线程模型：Connect() 之后启动一个后台接收线程；该线程阻塞读取并解析包，
// 把「响应 / 推送 / 断线」通过 PostMessage 投递到 notifyWnd（UI 线程）。
// 所有发送都由调用方（UI 线程）直接调用，socket 发送是线程安全的。
class PomeloClient {
public:
    PomeloClient();
    ~PomeloClient();

    PomeloClient(const PomeloClient&) = delete;
    PomeloClient& operator=(const PomeloClient&) = delete;

    // 连接到 host:port 并完成 pomelo 握手（阻塞，带超时）。失败返回 false。
    bool Connect(const std::string& host, uint16_t port);

    // 关闭连接并等待接收线程退出。
    void Close();

    // 发送请求，返回消息 id。响应会经 WM_APP_RESPONSE(wParam=id) 投递。
    uint32_t Request(const std::string& route, const std::string& jsonBody);

    // 发送通知（无响应）。
    void Notify(const std::string& route, const std::string& jsonBody);

    void SetNotifyWnd(HWND hwnd) { notifyWnd_ = hwnd; }
    bool IsConnected() const { return connected_.load(); }

private:
    void recvLoop();
    bool waitReadable(int timeoutMs);
    bool readExact(char* buf, int len);
    bool readPacketNoWait(uint8_t& type, std::vector<uint8_t>& body);
    void sendAll(const uint8_t* data, size_t len);
    void sendPacket(uint8_t type, const uint8_t* body, size_t len);
    void maybeSendHeartbeat();
    void processPacket(uint8_t type, const std::vector<uint8_t>& body);

    SOCKET sock_ = INVALID_SOCKET;
    HWND notifyWnd_ = nullptr;
    std::atomic<bool> running_{false};
    std::atomic<bool> connected_{false};
    std::atomic<uint32_t> nextId_{1};
    std::thread recvThread_;
    std::atomic<uint64_t> lastHeartbeatMs_{0};
};

uint64_t nowMs();
