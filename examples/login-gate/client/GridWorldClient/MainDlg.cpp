#include "pch.h"
#include "MainDlg.h"
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
    if (const auto* cx = v.find("cell_x")) o.cellX = cx->asInt();
    if (const auto* cy = v.find("cell_y")) o.cellY = cy->asInt();
    return o;
}

} // namespace

CMainDlg::CMainDlg(CWnd* pParent) : CDialogEx(IDD_MAIN_DIALOG, pParent) {}

void CMainDlg::DoDataExchange(CDataExchange* pDX) {
    CDialogEx::DoDataExchange(pDX);
}

BEGIN_MESSAGE_MAP(CMainDlg, CDialogEx)
    ON_WM_DESTROY()
    ON_WM_SIZE()
    ON_BN_CLICKED(IDC_CONNECT, &CMainDlg::OnConnect)
    ON_MESSAGE(WM_APP_RESPONSE, &CMainDlg::OnResponse)
    ON_MESSAGE(WM_APP_PUSH, &CMainDlg::OnPush)
    ON_MESSAGE(WM_APP_CLOSED, &CMainDlg::OnClosed)
END_MESSAGE_MAP()

BOOL CMainDlg::OnInitDialog() {
    CDialogEx::OnInitDialog();

    // 默认账号
    SetDlgItemText(IDC_ACCOUNT, _T("admin"));
    SetDlgItemText(IDC_PASSWORD, _T("admin123"));
    CheckRadioButton(IDC_RADIO_RED, IDC_RADIO_GREEN, IDC_RADIO_RED);

    // 登录控件区下方创建世界视图。
    CRect rc;
    GetClientRect(&rc);
    const int top = 70;
    CRect worldRect(0, top, rc.Width(), rc.Height());
    world_.Create(nullptr, _T(""), WS_CHILD | WS_VISIBLE | WS_TABSTOP, worldRect, this, IDC_WORLD);
    world_.SetCallbacks(
        [this](int x, int y) { onMoveTo(x, y); },
        [this](int cx, int cy) { onView(cx, cy); });

    client_.SetNotifyWnd(m_hWnd);
    setStatus(_T("未连接"));
    return TRUE;
}

void CMainDlg::OnDestroy() {
    client_.Close();
    CDialogEx::OnDestroy();
}

void CMainDlg::OnSize(UINT nType, int cx, int cy) {
    CDialogEx::OnSize(nType, cx, cy);
    if (world_.GetSafeHwnd()) {
        const int top = 70;
        world_.MoveWindow(0, top, cx, cy - top > 0 ? cy - top : 0, TRUE);
    }
}

void CMainDlg::OnConnect() {
    if (client_.IsConnected()) {
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

    if (!client_.Connect("127.0.0.1", 20011)) {
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
            GetDlgItem(IDC_CONNECT)->EnableWindow(TRUE);
            setStatus(_T("登录失败"));
            return;
        }
        json::Value v = json::parse(json);
        if (const auto* uid = v.find("uid")) uid_ = uid->asInt64();

        setStatus(_T("登录成功，进入世界..."));
        std::string enterBody = "{\"faction\":" + std::to_string(f) + "}";
        sendRequest("game.world.enter", enterBody,
                    [this](bool e, const std::string& j) { handleEnterResp(e, j); });
    });
}

void CMainDlg::sendRequest(const std::string& route, const std::string& body,
                           std::function<void(bool, const std::string&)> cb) {
    uint32_t id = client_.Request(route, body);
    pending_[id] = std::move(cb);
}

void CMainDlg::handleEnterResp(bool err, const std::string& json) {
    if (err) {
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
        GetDlgItem(IDC_CONNECT)->EnableWindow(TRUE);
        setStatus(_T("enter 响应缺少字段"));
        return;
    }

    std::string map = mapv->asString();
    int64_t selfId = self->asInt64();
    int spawnX = spx->asInt();
    int spawnY = spy->asInt();

    world_.SetMap(map);
    world_.SetSelf(selfId, spawnX, spawnY);

    if (const auto* objs = v.find("objects")) {
        if (objs->isArray()) {
            for (size_t i = 0; i < objs->size(); ++i) {
                world_.OnEnterObject(ParseWorldObject((*objs)[i]));
            }
        }
    }

    selfId_ = selfId;
    entered_ = true;
    setStatus(_T("已进入世界：点击格子移动，方向键平移视野"));
}

void CMainDlg::handleMoveToResp(bool err, const std::string& json) {
    if (err) return;
    json::Value v = json::parse(json);
    std::vector<std::pair<int, int>> path;
    if (const auto* p = v.find("path")) {
        if (p->isArray()) {
            for (size_t i = 0; i < p->size(); ++i) {
                int x = 0, y = 0;
                if (const auto* px = (*p)[i].find("x")) x = px->asInt();
                if (const auto* py = (*p)[i].find("y")) y = py->asInt();
                path.emplace_back(x, y);
            }
        }
    }
    world_.SetPath(path);
}

void CMainDlg::onMoveTo(int x, int y) {
    if (!entered_) return;
    std::string body = "{\"x\":" + std::to_string(x) + ",\"y\":" + std::to_string(y) + "}";
    sendRequest("game.world.moveTo", body,
                [this](bool err, const std::string& json) { handleMoveToResp(err, json); });
}

void CMainDlg::onView(int cx, int cy) {
    if (!entered_) return;
    std::string body = "{\"x\":" + std::to_string(cx) + ",\"y\":" + std::to_string(cy) + "}";
    sendRequest("game.world.view", body, [](bool, const std::string&) {});
}

LRESULT CMainDlg::OnResponse(WPARAM w, LPARAM l) {
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

LRESULT CMainDlg::OnPush(WPARAM, LPARAM l) {
    PushPayload* p = reinterpret_cast<PushPayload*>(l);
    if (p->route == "game.world.objEnter") {
        json::Value v = json::parse(p->json);
        world_.OnEnterObject(ParseWorldObject(v));
    } else if (p->route == "game.world.objMove") {
        json::Value v = json::parse(p->json);
        int64_t id = 0;
        int cx = 0, cy = 0;
        if (const auto* idv = v.find("id")) id = idv->asInt64();
        if (const auto* cxv = v.find("cell_x")) cx = cxv->asInt();
        if (const auto* cyv = v.find("cell_y")) cy = cyv->asInt();
        world_.OnMoveObject(id, cx, cy);
    } else if (p->route == "game.world.objLeave") {
        json::Value v = json::parse(p->json);
        int64_t id = 0;
        if (const auto* idv = v.find("id")) id = idv->asInt64();
        world_.OnLeaveObject(id);
    }
    delete p;
    return 0;
}

LRESULT CMainDlg::OnClosed(WPARAM, LPARAM) {
    GetDlgItem(IDC_CONNECT)->EnableWindow(TRUE);
    entered_ = false;
    pending_.clear();
    setStatus(_T("连接已断开"));
    return 0;
}

void CMainDlg::setStatus(const CString& text) {
    SetDlgItemText(IDC_STATUS, text);
}
