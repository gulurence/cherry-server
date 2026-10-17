#include "pch.h"
#include "LoginDlg.h"
#include "Json.h"
#include "resource.h"

namespace {

// 宽字符 CString -> UTF-8 std::string。
std::string ToUtf8(const CString& s) {
    int n = ::WideCharToMultiByte(CP_UTF8, 0, s, s.GetLength(), nullptr, 0, nullptr, nullptr);
    if (n <= 0) return std::string();
    std::string out(static_cast<size_t>(n), '\0');
    ::WideCharToMultiByte(CP_UTF8, 0, s, s.GetLength(), &out[0], n, nullptr, nullptr);
    return out;
}

WorldObj ParseWorldObject(const json::Value& v) {
    WorldObj o;
    if (const auto* id = v.find("id")) o.id = id->asInt64();
    if (const auto* kind = v.find("kind")) o.kind = kind->asInt();
    if (const auto* faction = v.find("faction")) o.faction = faction->asInt();
    if (const auto* sz = v.find("size")) o.size = sz->asInt();
    if (const auto* cx = v.find("cell_x")) o.cellX = cx->asInt();
    if (const auto* cy = v.find("cell_y")) o.cellY = cy->asInt();
    return o;
}

} // namespace

CLoginDlg::CLoginDlg(CWnd* pParent) : CDialogEx(IDD_LOGIN_DIALOG, pParent) {}

void CLoginDlg::DoDataExchange(CDataExchange* pDX) {
    CDialogEx::DoDataExchange(pDX);
}

BEGIN_MESSAGE_MAP(CLoginDlg, CDialogEx)
    ON_WM_DESTROY()
    ON_BN_CLICKED(IDC_CONNECT, &CLoginDlg::OnConnect)
    ON_MESSAGE(WM_APP_RESPONSE, &CLoginDlg::OnResponse)
    ON_MESSAGE(WM_APP_CLOSED, &CLoginDlg::OnClosed)
END_MESSAGE_MAP()

BOOL CLoginDlg::OnInitDialog() {
    CDialogEx::OnInitDialog();

    // 预填演示账号，默认红方。
    SetDlgItemText(IDC_ACCOUNT, _T("admin"));
    SetDlgItemText(IDC_PASSWORD, _T("admin123"));
    CheckRadioButton(IDC_RADIO_RED, IDC_RADIO_GREEN, IDC_RADIO_RED);
    setStatus(_T("未连接"));
    return TRUE;
}

void CLoginDlg::OnDestroy() {
    // 进入成功后连接要移交给主窗口；只有取消/失败时才在这里断开。
    if (!success_) {
        resetConnection();
    }
    CDialogEx::OnDestroy();
}

void CLoginDlg::OnConnect() {
    if (client_) {
        setStatus(_T("已连接"));
        return;
    }

    CString account, password;
    GetDlgItemText(IDC_ACCOUNT, account);
    GetDlgItemText(IDC_PASSWORD, password);

    int faction = 0;
    int checked = GetCheckedRadioButton(IDC_RADIO_RED, IDC_RADIO_GREEN);
    if (checked == IDC_RADIO_BLACK) faction = 1;
    else if (checked == IDC_RADIO_GREEN) faction = 2;

    std::string acct = ToUtf8(account);
    std::string pwd = ToUtf8(password);

    setStatus(_T("连接中..."));
    GetDlgItem(IDC_CONNECT)->EnableWindow(FALSE);

    client_ = std::make_unique<PomeloClient>();
    client_->SetNotifyWnd(m_hWnd);

    if (!client_->Connect("127.0.0.1", 20011)) {
        resetConnection();
        GetDlgItem(IDC_CONNECT)->EnableWindow(TRUE);
        setStatus(_T("连接失败"));
        return;
    }

    setStatus(_T("已连接，登录中..."));

    std::string body = "{\"account\":\"" + json::escape(acct) +
                       "\",\"password\":\"" + json::escape(pwd) + "\"}";
    int f = faction; // 捕获当前阵营，供进入世界使用
    sendRequest("gate.user.login", body, [this, f](bool err, const std::string& json) {
        if (err) {
            resetConnection();
            GetDlgItem(IDC_CONNECT)->EnableWindow(TRUE);
            setStatus(_T("登录失败"));
            return;
        }
        setStatus(_T("登录成功，进入世界..."));
        info_.faction = f;
        std::string enterBody = "{\"faction\":" + std::to_string(f) + "}";
        sendRequest("game.world.enter", enterBody,
                    [this](bool e, const std::string& j) { handleEnterResp(e, j); });
    });
}

void CLoginDlg::sendRequest(const std::string& route, const std::string& body,
                            std::function<void(bool, const std::string&)> cb) {
    uint32_t id = client_->Request(route, body);
    pending_[id] = std::move(cb);
}

void CLoginDlg::handleEnterResp(bool err, const std::string& json) {
    if (err) {
        resetConnection();
        GetDlgItem(IDC_CONNECT)->EnableWindow(TRUE);
        setStatus(_T("进入世界失败"));
        return;
    }

    json::Value v = json::parse(json);
    const auto* mapv = v.find("map");
    const auto* self = v.find("self_id");
    const auto* spx = v.find("spawn_x");
    const auto* spy = v.find("spawn_y");
    if (!mapv || !self || !spx || !spy) {
        resetConnection();
        GetDlgItem(IDC_CONNECT)->EnableWindow(TRUE);
        setStatus(_T("enter 响应缺少字段"));
        return;
    }

    info_.map = mapv->asString();
    info_.selfId = self->asInt64();
    info_.spawnX = spx->asInt();
    info_.spawnY = spy->asInt();

    if (const auto* objs = v.find("objects")) {
        if (objs->isArray()) {
            for (size_t i = 0; i < objs->size(); ++i) {
                info_.objects.push_back(ParseWorldObject((*objs)[i]));
            }
        }
    }

    success_ = true;
    EndDialog(IDOK);
}

void CLoginDlg::resetConnection() {
    pending_.clear();
    if (client_) {
        client_->Close();
        client_.reset();
    }
}

LRESULT CLoginDlg::OnResponse(WPARAM w, LPARAM l) {
    uint32_t id = static_cast<uint32_t>(w);
    ResponsePayload* p = reinterpret_cast<ResponsePayload*>(l);
    auto it = pending_.find(id);
    if (it != pending_.end()) {
        auto cb = std::move(it->second);
        pending_.erase(it);
        cb(p->isError, p->json);
    }
    delete p;
    return 0;
}

LRESULT CLoginDlg::OnClosed(WPARAM, LPARAM) {
    resetConnection();
    GetDlgItem(IDC_CONNECT)->EnableWindow(TRUE);
    setStatus(_T("连接已断开"));
    return 0;
}

void CLoginDlg::setStatus(const CString& text) {
    SetDlgItemText(IDC_STATUS, text);
}
