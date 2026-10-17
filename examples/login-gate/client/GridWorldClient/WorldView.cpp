#include "pch.h"
#include "WorldView.h"

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

    // 1. 地形
    for (int wy = 0; wy < r; ++wy) {
        for (int wx = 0; wx < c; ++wx) {
            int cx = viewCx_ - c / 2 + wx;
            int cy = viewCy_ - r / 2 + wy;
            COLORREF color;
            if (cx < 0 || cy < 0 || cx >= MapW || cy >= MapH) {
                color = RGB(10, 10, 10);
            } else if (map_.empty()) {
                color = RGB(200, 210, 190);
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
        int midX = 0, midY = 0;
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
            if (i == path.size() / 2) {
                midX = px;
                midY = py;
            }
        }
        memDC.SelectObject(oldPathPen);

        CString txt;
        txt.Format(_T("%d ms"), costMs);
        int oldBk = memDC.SetBkMode(TRANSPARENT);
        COLORREF oldText = memDC.SetTextColor(RGB(255, 220, 0));
        memDC.TextOut(midX, midY - 14, txt);
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

    dc.BitBlt(0, 0, rc.Width(), rc.Height(), &memDC, 0, 0, SRCCOPY);
    memDC.SelectObject(oldBmp);
}

void CWorldView::OnLButtonDown(UINT /*nFlags*/, CPoint point) {
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
