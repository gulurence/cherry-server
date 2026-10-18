#include "pch.h"
#include "MainDlg.h"
#include "Json.h"
#include "resource.h"

#include <chrono>

namespace {

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

CMainDlg::CMainDlg(std::unique_ptr<PomeloClient> client, EnterInfo info, CWnd* pParent)
    : CDialogEx(IDD_MAIN_DIALOG, pParent), client_(std::move(client)), info_(std::move(info)) {}

void CMainDlg::DoDataExchange(CDataExchange* pDX) {
    CDialogEx::DoDataExchange(pDX);
}

BEGIN_MESSAGE_MAP(CMainDlg, CDialogEx)
    ON_WM_DESTROY()
    ON_WM_SIZE()
    ON_MESSAGE(WM_APP_RESPONSE, &CMainDlg::OnResponse)
    ON_MESSAGE(WM_APP_PUSH, &CMainDlg::OnPush)
    ON_MESSAGE(WM_APP_CLOSED, &CMainDlg::OnClosed)
    ON_MESSAGE(WM_APP_PATH, &CMainDlg::OnPathReady)
END_MESSAGE_MAP()

BOOL CMainDlg::OnInitDialog() {
    CDialogEx::OnInitDialog();

    // 窗口默认大小：客户端每格 30px（放大 10 倍），世界视图 2250×1260 显示 75×42 格，
    // 加顶部状态栏；方向键平移视野。
    const int top = 24;
    const int viewW = 2250; // 75 格 × 30px
    const int viewH = 1260; // 42 格 × 30px
    CRect wnd(0, 0, viewW, top + viewH);
    ::AdjustWindowRect(&wnd, GetStyle(), FALSE);
    SetWindowPos(nullptr, 0, 0, wnd.Width(), wnd.Height(), SWP_NOMOVE | SWP_NOZORDER);

    // 世界视图填充状态栏下方的整个客户区。
    CRect rc;
    GetClientRect(&rc);
    CRect worldRect(0, top, rc.Width(), rc.Height());
    world_.Create(nullptr, _T(""), WS_CHILD | WS_VISIBLE | WS_TABSTOP, worldRect, this, IDC_WORLD);
    world_.SetCallbacks(
        [this](int x, int y) { onMoveTo(x, y); },
        [this](int cx, int cy) { onView(cx, cy); });

    // 接管登录窗口移交过来的连接，并把进入数据喂给世界视图。
    client_->SetNotifyWnd(m_hWnd);
    world_.SetMap(info_.map);
    world_.SetSelf(info_.selfId, info_.spawnX, info_.spawnY, info_.faction);
    world_.SetCastles(info_.castles);
    for (const auto& o : info_.objects) {
        world_.OnEnterObject(o);
    }
    selfId_ = info_.selfId;

    setStatus(_T("已进入世界"));
    return TRUE;
}

void CMainDlg::OnDestroy() {
    if (client_) {
        client_->Close();
        client_.reset();
    }
    CDialogEx::OnDestroy();
}

void CMainDlg::OnSize(UINT nType, int cx, int cy) {
    CDialogEx::OnSize(nType, cx, cy);
    if (world_.GetSafeHwnd()) {
        const int top = 24;
        world_.MoveWindow(0, top, cx, cy - top > 0 ? cy - top : 0, TRUE);
    }
}

void CMainDlg::sendRequest(const std::string& route, const std::string& body,
                           std::function<void(bool, const std::string&)> cb) {
    uint32_t id = client_->Request(route, body);
    pending_[id] = std::move(cb);
}

void CMainDlg::handleMoveToResp(bool err, const std::string& json) {
    if (err) return;
    json::Value v = json::parse(json);
    // 客户端本地已预寻路，显示用本地耗时；服务端返回的 cost_ms 只是校验耗时（≈0）。
    int costMs = selfPathCostMs_;
    if (costMs <= 0) {
        if (const auto* c = v.find("cost_ms")) costMs = c->asInt();
    }
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
    world_.OnObjPath(selfId_, costMs, path);
}

void CMainDlg::onMoveTo(int x, int y) {
    // 目标不可通行则本地直接忽略（不发请求）。
    const std::string& map = world_.GetMap();
    if (!map.empty()) {
        if (x < 0 || y < 0 || x >= CWorldView::MapW || y >= CWorldView::MapH) return;
        if (map[(size_t)y * CWorldView::MapW + x] != '0') return;
    }

    auto self = world_.GetSelfCell();
    int sx = self.first, sy = self.second;
    std::string mapCopy = map;
    HWND hwnd = m_hWnd;

    // 后台线程本地 A*（避免卡 UI），完成后经 WM_APP_PATH 回投 UI 线程。
    std::thread([hwnd, mapCopy, sx, sy, x, y]() {
        auto t0 = std::chrono::steady_clock::now();
        auto path = CWorldView::FindPath(mapCopy, sx, sy, x, y);
        auto costMs = std::chrono::duration_cast<std::chrono::milliseconds>(
                          std::chrono::steady_clock::now() - t0)
                          .count();
        auto* res = new PathResult;
        res->tx = x;
        res->ty = y;
        res->costMs = static_cast<int>(costMs);
        res->path = std::move(path);
        ::PostMessage(hwnd, WM_APP_PATH, 0, reinterpret_cast<LPARAM>(res));
    }).detach();
}

LRESULT CMainDlg::OnPathReady(WPARAM, LPARAM l) {
    std::unique_ptr<PathResult> res(reinterpret_cast<PathResult*>(l));
    if (!res) return 0;

    selfPathCostMs_ = res->costMs;

    std::string body = "{\"x\":" + std::to_string(res->tx) +
                       ",\"y\":" + std::to_string(res->ty);
    if (!res->path.empty()) {
        body += ",\"path\":[";
        for (size_t i = 0; i < res->path.size(); ++i) {
            if (i) body += ",";
            body += "{\"x\":" + std::to_string(res->path[i].first) +
                    ",\"y\":" + std::to_string(res->path[i].second) + "}";
        }
        body += "]";
    }
    body += "}";

    sendRequest("game.world.moveTo", body,
                [this](bool err, const std::string& json) { handleMoveToResp(err, json); });
    return 0;
}

void CMainDlg::onView(int cx, int cy) {
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
    } else if (p->route == "game.world.objPath") {
        json::Value v = json::parse(p->json);
        int64_t id = 0;
        int costMs = 0;
        std::vector<std::pair<int, int>> path;
        if (const auto* idv = v.find("id")) id = idv->asInt64();
        if (const auto* c = v.find("cost_ms")) costMs = c->asInt();
        if (const auto* pp = v.find("path")) {
            if (pp->isArray()) {
                for (size_t i = 0; i < pp->size(); ++i) {
                    int x = 0, y = 0;
                    if (const auto* px = (*pp)[i].find("x")) x = px->asInt();
                    if (const auto* py = (*pp)[i].find("y")) y = py->asInt();
                    path.emplace_back(x, y);
                }
            }
        }
        world_.OnObjPath(id, costMs, path);
    } else if (p->route == "game.world.castleAdd" || p->route == "game.world.castleRemove") {
        json::Value v = json::parse(p->json);
        std::vector<std::pair<int, int>> cells;
        if (const auto* cs = v.find("cells")) {
            if (cs->isArray()) {
                for (size_t i = 0; i < cs->size(); ++i) {
                    int x = 0, y = 0;
                    if (const auto* px = (*cs)[i].find("x")) x = px->asInt();
                    if (const auto* py = (*cs)[i].find("y")) y = py->asInt();
                    cells.emplace_back(x, y);
                }
            }
        }
        if (p->route == "game.world.castleAdd") world_.OnCastleAdd(cells);
        else world_.OnCastleRemove(cells);
    } else if (p->route == "game.world.castleTower") {
        json::Value v = json::parse(p->json);
        int tower = 0;
        std::vector<std::pair<int, int>> cells;
        if (const auto* tv = v.find("tower")) tower = tv->asInt();
        if (const auto* cs = v.find("cells")) {
            if (cs->isArray()) {
                for (size_t i = 0; i < cs->size(); ++i) {
                    int x = 0, y = 0;
                    if (const auto* px = (*cs)[i].find("x")) x = px->asInt();
                    if (const auto* py = (*cs)[i].find("y")) y = py->asInt();
                    cells.emplace_back(x, y);
                }
            }
        }
        world_.OnCastleTower(tower, cells);
    }
    delete p;
    return 0;
}

LRESULT CMainDlg::OnClosed(WPARAM, LPARAM) {
    pending_.clear();
    setStatus(_T("连接已断开"));
    return 0;
}

void CMainDlg::setStatus(const CString& text) {
    SetDlgItemText(IDC_STATUS, text);
}
