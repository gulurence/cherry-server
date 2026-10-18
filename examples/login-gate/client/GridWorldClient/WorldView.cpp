#include "pch.h"
#include "WorldView.h"

#include <algorithm>
#include <climits>
#include <queue>

namespace {
// 左上角缩略图：边长与距客户区左上角的边距（像素）。
const int kMinimapSize = 200;
const int kMinimapMargin = 8;
} // namespace

BEGIN_MESSAGE_MAP(CWorldView, CWnd)
    ON_WM_PAINT()
    ON_WM_ERASEBKGND()
    ON_WM_LBUTTONDOWN()
    ON_WM_KEYDOWN()
    ON_WM_GETDLGCODE()
END_MESSAGE_MAP()

int CWorldView::cols() const {
    CRect rc;
    GetClientRect(&rc);
    int c = rc.Width() / cellSize_;
    return c > 0 ? c : 1;
}

int CWorldView::rows() const {
    CRect rc;
    GetClientRect(&rc);
    int r = rc.Height() / cellSize_;
    return r > 0 ? r : 1;
}

// 世界格子 (cx,cy) -> 屏幕上该格子左上角的像素坐标。
void CWorldView::worldToScreen(int cx, int cy, int& sx, int& sy) const {
    sx = (cx - (viewCx_ - cols() / 2)) * cellSize_;
    sy = (cy - (viewCy_ - rows() / 2)) * cellSize_;
}

// 屏幕像素 -> 世界格子坐标。
void CWorldView::screenToWorld(int sx, int sy, int& cx, int& cy) const {
    cx = sx / cellSize_ + (viewCx_ - cols() / 2);
    cy = sy / cellSize_ + (viewCy_ - rows() / 2);
}

void CWorldView::setCastle(int x, int y, bool blocked) {
    if (x < 0 || y < 0 || x >= MapW || y >= MapH) return;
    if (castleMap_.size() != (size_t)MapW * MapH) return;
    castleMap_[(size_t)y * MapW + x] = blocked ? '1' : '0';
}

// OnCastleTower 处理灯塔视野切换时的城堡快照：服务端下发 tower 编号（ty*towerW+tx），
// 客户端先清空该 50×50 塔区域，再按 cells 把当前城堡格重设为 '1'，避免视野外
// 城堡增删漏收导致的新旧城堡残留。
void CWorldView::OnCastleTower(int tower, const std::vector<std::pair<int, int>>& cells) {
    if (castleMap_.size() != (size_t)MapW * MapH) return;

    // 与服务端灯塔 AOI 一致：塔大小 50×50 格，世界 20×20 塔。
    const int towerSize = 50;
    const int towerW = MapW / towerSize; // 20

    int tx = tower % towerW;
    int ty = tower / towerW;
    for (int y = ty * towerSize; y < (ty + 1) * towerSize && y < MapH; ++y) {
        for (int x = tx * towerSize; x < (tx + 1) * towerSize && x < MapW; ++x) {
            castleMap_[(size_t)y * MapW + x] = '0';
        }
    }
    for (const auto& c : cells) {
        setCastle(c.first, c.second, true);
    }
    if (IsWindow(m_hWnd)) Invalidate(FALSE);
}

CRect CWorldView::minimapRect() const {
    return CRect(kMinimapMargin, kMinimapMargin,
                 kMinimapMargin + kMinimapSize, kMinimapMargin + kMinimapSize);
}

// drawMinimap 在客户区左上角绘制半透明缩略图：整张地图按比例缩小、已知对象圆点、
// 以及当前视野框。缩略图先画到临时位图，再用 AlphaBlend 统一半透明合成。
void CWorldView::drawMinimap(CDC& dc) {
    const CRect rect = minimapRect();

    CDC miniDC;
    miniDC.CreateCompatibleDC(&dc);
    CBitmap miniBmp;
    miniBmp.CreateCompatibleBitmap(&dc, kMinimapSize, kMinimapSize);
    CBitmap* oldMini = miniDC.SelectObject(&miniBmp);

    const bool hasMap = !map_.empty();
    const bool hasCastle = castleMap_.size() == (size_t)MapW * MapH;

    // 1. 地形（每个缩略图像素采样对应区域左上角的一个格子）
    for (int py = 0; py < kMinimapSize; ++py) {
        int cy = py * MapH / kMinimapSize;
        for (int px = 0; px < kMinimapSize; ++px) {
            int cx = px * MapW / kMinimapSize;
            COLORREF color = RGB(200, 210, 190); // 可行走
            if (hasMap) {
                if (hasCastle && castleMap_[(size_t)cy * MapW + cx] == '1') {
                    color = RGB(96, 72, 48); // 城堡
                } else {
                    char t = map_[(size_t)cy * MapW + cx];
                    switch (t) {
                        case '1': color = RGB(105, 105, 105); break; // 山
                        case '2': color = RGB(60, 120, 200); break;  // 河
                        default: color = RGB(200, 210, 190); break;
                    }
                }
            }
            miniDC.SetPixel(px, py, color);
        }
    }

    // 2. 已知对象圆点（自己用白色高亮）
    for (const auto& kv : objs_) {
        const WorldObj& o = kv.second;
        int mx = o.cellX * kMinimapSize / MapW;
        int my = o.cellY * kMinimapSize / MapH;
        COLORREF c = (o.id == selfId_) ? RGB(255, 255, 255) : factionColor(o.faction);
        miniDC.FillSolidRect(mx - 1, my - 1, 3, 3, c);
    }

    // 3. 当前视野框
    int vx0 = (viewCx_ - cols() / 2) * kMinimapSize / MapW;
    int vy0 = (viewCy_ - rows() / 2) * kMinimapSize / MapH;
    int vx1 = (viewCx_ + cols() / 2) * kMinimapSize / MapW;
    int vy1 = (viewCy_ + rows() / 2) * kMinimapSize / MapH;
    if (vx0 < 0) vx0 = 0;
    if (vy0 < 0) vy0 = 0;
    if (vx1 >= kMinimapSize) vx1 = kMinimapSize - 1;
    if (vy1 >= kMinimapSize) vy1 = kMinimapSize - 1;

    CPen vpPen(PS_SOLID, 1, RGB(255, 255, 255));
    CPen* oldVp = miniDC.SelectObject(&vpPen);
    CBrush* oldVpBr = static_cast<CBrush*>(miniDC.SelectStockObject(NULL_BRUSH));
    miniDC.Rectangle(vx0, vy0, vx1, vy1);
    miniDC.SelectObject(oldVp);
    miniDC.SelectObject(oldVpBr);

    // 4. 半透明合成（AlphaFormat=0：源无 alpha，统一按 SourceConstantAlpha≈70% 混合）
    BLENDFUNCTION bf;
    bf.BlendOp = AC_SRC_OVER;
    bf.BlendFlags = 0;
    bf.SourceConstantAlpha = 180;
    bf.AlphaFormat = 0;
    dc.AlphaBlend(rect.left, rect.top, kMinimapSize, kMinimapSize,
                  &miniDC, 0, 0, kMinimapSize, kMinimapSize, bf);

    miniDC.SelectObject(oldMini);
}

COLORREF CWorldView::factionColor(int faction) const {
    switch (faction) {
        case 0: return RGB(210, 60, 60);   // 红
        case 1: return RGB(35, 35, 35);    // 黑
        case 2: return RGB(60, 170, 70);   // 绿
        default: return RGB(160, 160, 160);
    }
}

BOOL CWorldView::OnEraseBkgnd(CDC* /*pDC*/) {
    return TRUE; // 由 OnPaint 整体双缓冲绘制，避免闪烁
}

void CWorldView::OnPaint() {
    CPaintDC dc(this);
    CRect rc;
    GetClientRect(&rc);

    CDC memDC;
    memDC.CreateCompatibleDC(&dc);
    CBitmap bmp;
    bmp.CreateCompatibleBitmap(&dc, rc.Width(), rc.Height());
    CBitmap* oldBmp = memDC.SelectObject(&bmp);

    memDC.FillSolidRect(&rc, RGB(15, 15, 15));

    const int c = cols();
    const int r = rows();

    // 1. 地形（动态城堡覆盖在可行走地形之上）
    for (int wy = 0; wy < r; ++wy) {
        for (int wx = 0; wx < c; ++wx) {
            int cx = viewCx_ - c / 2 + wx;
            int cy = viewCy_ - r / 2 + wy;
            COLORREF color;
            if (cx < 0 || cy < 0 || cx >= MapW || cy >= MapH) {
                color = RGB(10, 10, 10);
            } else if (map_.empty()) {
                color = RGB(200, 210, 190);
            } else if (castleMap_.size() == (size_t)MapW * MapH &&
                       castleMap_[(size_t)cy * MapW + cx] == '1') {
                color = RGB(96, 72, 48); // 城堡
            } else {
                char t = map_[(size_t)cy * MapW + cx];
                switch (t) {
                    case '1': color = RGB(105, 105, 105); break; // 山
                    case '2': color = RGB(60, 120, 200); break;  // 河
                    default: color = RGB(200, 210, 190); break;  // 可行走
                }
            }
            memDC.FillSolidRect(wx * cellSize_, wy * cellSize_, cellSize_, cellSize_, color);
        }
    }

    // 2. 网格线
    CPen gridPen(PS_SOLID, 1, RGB(185, 190, 180));
    CPen* oldPen = memDC.SelectObject(&gridPen);
    for (int wx = 0; wx <= c; ++wx) {
        memDC.MoveTo(wx * cellSize_, 0);
        memDC.LineTo(wx * cellSize_, r * cellSize_);
    }
    for (int wy = 0; wy <= r; ++wy) {
        memDC.MoveTo(0, wy * cellSize_);
        memDC.LineTo(c * cellSize_, wy * cellSize_);
    }
    memDC.SelectObject(oldPen);

    // 3. 每个对象的路径折线（青蓝色）+ 线上显示寻路耗时(ms)
    for (const auto& kv : paths_) {
        const auto& path = kv.second;
        if (path.size() < 2) continue;
        int costMs = 0;
        auto cit = pathCostMs_.find(kv.first);
        if (cit != pathCostMs_.end()) costMs = cit->second;

        CPen pathPen(PS_SOLID, 1, RGB(0, 200, 220));
        CPen* oldPathPen = memDC.SelectObject(&pathPen);
        bool first = true;
        for (size_t i = 0; i < path.size(); ++i) {
            int sx, sy;
            worldToScreen(path[i].first, path[i].second, sx, sy);
            int px = sx + cellSize_ / 2;
            int py = sy + cellSize_ / 2;
            if (first) {
                memDC.MoveTo(px, py);
                first = false;
            } else {
                memDC.LineTo(px, py);
            }
        }
        memDC.SelectObject(oldPathPen);

        // 寻路耗时显示在路径终点格上方。
        int endSx, endSy;
        worldToScreen(path.back().first, path.back().second, endSx, endSy);

        CString txt;
        txt.Format(_T("%d ms / %d 路点"), costMs, (int)path.size());
        int oldBk = memDC.SetBkMode(TRANSPARENT);
        COLORREF oldText = memDC.SetTextColor(RGB(255, 220, 0));
        memDC.TextOut(endSx + cellSize_ / 2, endSy + cellSize_ / 2 - 14, txt);
        memDC.SetTextColor(oldText);
        memDC.SetBkMode(oldBk);
    }

    // 4. 对象（按尺寸 size 绘制 cellSize_*size 像素的方块，中心对齐所在格）
    for (const auto& kv : objs_) {
        const WorldObj& o = kv.second;
        int sx, sy;
        worldToScreen(o.cellX, o.cellY, sx, sy);

        int objPx = cellSize_ * (o.size > 0 ? o.size : 1);
        int left = sx + cellSize_ / 2 - objPx / 2;
        int top = sy + cellSize_ / 2 - objPx / 2;
        if (left + objPx < 0 || top + objPx < 0 || left > rc.Width() || top > rc.Height()) {
            continue;
        }

        COLORREF col = factionColor(o.faction);

        // 黑方画白描边，避免看不清
        if (o.faction == 1) {
            memDC.FillSolidRect(left - 1, top - 1, objPx + 2, objPx + 2, RGB(255, 255, 255));
        }

        memDC.FillSolidRect(left, top, objPx, objPx, col);

        // 自己：白色方框高亮
        if (o.id == selfId_) {
            CPen selfPen(PS_SOLID, 2, RGB(255, 255, 255));
            CPen* oldSP = memDC.SelectObject(&selfPen);
            CBrush* oldSB = static_cast<CBrush*>(memDC.SelectStockObject(NULL_BRUSH));
            memDC.Rectangle(left - 2, top - 2, left + objPx + 2, top + objPx + 2);
            memDC.SelectObject(oldSP);
            memDC.SelectObject(oldSB);
        }
    }

    // 5. 左上角半透明缩略图（最后叠加）
    drawMinimap(memDC);

    dc.BitBlt(0, 0, rc.Width(), rc.Height(), &memDC, 0, 0, SRCCOPY);
    memDC.SelectObject(oldBmp);
}

void CWorldView::OnLButtonDown(UINT /*nFlags*/, CPoint point) {
    // 点击左上角缩略图：换算成世界格并移动到该位置。
    CRect mm = minimapRect();
    if (mm.PtInRect(point)) {
        int cx = (point.x - mm.left) * MapW / kMinimapSize;
        int cy = (point.y - mm.top) * MapH / kMinimapSize;
        if (cx < 0) cx = 0;
        if (cx >= MapW) cx = MapW - 1;
        if (cy < 0) cy = 0;
        if (cy >= MapH) cy = MapH - 1;
        if (onMoveTo_) onMoveTo_(cx, cy);
        return;
    }

    int cx, cy;
    screenToWorld(point.x, point.y, cx, cy);
    if (cx < 0 || cy < 0 || cx >= MapW || cy >= MapH) return;
    if (onMoveTo_) onMoveTo_(cx, cy);
}

void CWorldView::OnKeyDown(UINT nChar, UINT /*nRepCnt*/, UINT /*nFlags*/) {
    const int pan = 8; // 每次平移的格子数
    switch (nChar) {
        case VK_LEFT: viewCx_ -= pan; break;
        case VK_RIGHT: viewCx_ += pan; break;
        case VK_UP: viewCy_ -= pan; break;
        case VK_DOWN: viewCy_ += pan; break;
        default: return;
    }
    if (viewCx_ < 0) viewCx_ = 0;
    if (viewCx_ >= MapW) viewCx_ = MapW - 1;
    if (viewCy_ < 0) viewCy_ = 0;
    if (viewCy_ >= MapH) viewCy_ = MapH - 1;

    Invalidate(FALSE);
    if (onView_) onView_(viewCx_, viewCy_);
}

UINT CWorldView::OnGetDlgCode() {
    // 让方向键、字符键能够路由到本控件。
    return DLGC_WANTARROWS | DLGC_WANTCHARS;
}

namespace {

struct AStarNode {
    int f;
    int x;
    int y;
    bool operator<(const AStarNode& o) const { return f > o.f; } // 最小堆
};

inline int octileDist(int dx, int dy) {
    if (dx < 0) dx = -dx;
    if (dy < 0) dy = -dy;
    return dx > dy ? 10 * (dx - dy) + 14 * dy : 10 * (dy - dx) + 14 * dx;
}

} // namespace

// 与服务器 worldsim 同款 8 方向 A*（Octile 启发，斜向不穿墙角），返回完整路点。
std::vector<std::pair<int, int>> CWorldView::FindPath(const std::string& map,
                                                      int sx, int sy, int gx, int gy) {
    const int W = MapW, H = MapH;
    auto walkable = [&](int x, int y) {
        return x >= 0 && x < W && y >= 0 && y < H && map[(size_t)y * W + x] == '0';
    };
    if (!walkable(sx, sy) || !walkable(gx, gy)) return {};
    if (sx == gx && sy == gy) return {{sx, sy}};

    std::vector<int> gScore(W * H, INT_MAX);
    std::vector<int> parent(W * H, -1);
    std::vector<char> closed(W * H, 0);
    std::priority_queue<AStarNode> open;

    auto idx = [&](int x, int y) { return y * W + x; };
    gScore[idx(sx, sy)] = 0;
    open.push({octileDist(sx - gx, sy - gy), sx, sy});

    const int dx8[8] = {1, -1, 0, 0, 1, 1, -1, -1};
    const int dy8[8] = {0, 0, 1, -1, 1, -1, 1, -1};

    while (!open.empty()) {
        AStarNode cur = open.top();
        open.pop();
        int ci = idx(cur.x, cur.y);
        if (closed[ci]) continue;
        closed[ci] = 1;
        if (cur.x == gx && cur.y == gy) break;

        int baseG = gScore[ci];
        for (int i = 0; i < 8; ++i) {
            int nx = cur.x + dx8[i];
            int ny = cur.y + dy8[i];
            if (!walkable(nx, ny)) continue;
            if (i >= 4 && (!walkable(cur.x + dx8[i], cur.y) ||
                           !walkable(cur.x, cur.y + dy8[i]))) {
                continue; // 斜向不穿墙角
            }
            int ni = idx(nx, ny);
            if (closed[ni]) continue;
            int step = (i >= 4) ? 14 : 10;
            int ng = baseG + step;
            if (ng < gScore[ni]) {
                gScore[ni] = ng;
                parent[ni] = ci;
                open.push({ng + octileDist(nx - gx, ny - gy), nx, ny});
            }
        }
    }

    int gi = idx(gx, gy);
    if (gScore[gi] == INT_MAX) return {}; // 不可达

    std::vector<std::pair<int, int>> path;
    for (int i = gi; i != -1; i = parent[i]) {
        path.emplace_back(i % W, i / W);
    }
    std::reverse(path.begin(), path.end());
    return path;
}
