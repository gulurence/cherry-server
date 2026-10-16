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

    // 3. 自己的路径（青蓝色折线）
    if (!ownPath_.empty()) {
        CPen pathPen(PS_SOLID, 2, RGB(0, 200, 220));
        CPen* oldPathPen = memDC.SelectObject(&pathPen);
        bool first = true;
        int prevX = 0, prevY = 0;
        for (const auto& cell : ownPath_) {
            int sx, sy;
            worldToScreen(cell.first, cell.second, sx, sy);
            int px = sx + cellSize_ / 2;
            int py = sy + cellSize_ / 2;
            if (first) {
                memDC.MoveTo(px, py);
                first = false;
            } else {
                memDC.LineTo(px, py);
            }
            prevX = px;
            prevY = py;
        }
        (void)prevX;
        (void)prevY;
        memDC.SelectObject(oldPathPen);
    }

    // 4. 对象
    for (const auto& kv : objs_) {
        const WorldObj& o = kv.second;
        int sx, sy;
        worldToScreen(o.cellX, o.cellY, sx, sy);
        if (sx < -cellSize_ || sy < -cellSize_ || sx > rc.Width() || sy > rc.Height()) {
            continue;
        }

        COLORREF col = factionColor(o.faction);
        int margin = (o.kind == 1) ? 2 : 3; // 玩家稍大

        // 黑方画白描边，避免看不清
        if (o.faction == 1) {
            CBrush ring(RGB(255, 255, 255));
            CBrush* oldBr = memDC.SelectObject(&ring);
            CPen ringPen(PS_SOLID, 1, RGB(255, 255, 255));
            CPen* oldP = memDC.SelectObject(&ringPen);
            memDC.Ellipse(sx + margin - 1, sy + margin - 1,
                          sx + cellSize_ - margin + 1, sy + cellSize_ - margin + 1);
            memDC.SelectObject(oldP);
            memDC.SelectObject(oldBr);
        }

        CBrush brush(col);
        CBrush* oldBr = memDC.SelectObject(&brush);
        CPen pen(PS_SOLID, 1, col);
        CPen* oldP = memDC.SelectObject(&pen);
        memDC.Ellipse(sx + margin, sy + margin, sx + cellSize_ - margin, sy + cellSize_ - margin);
        memDC.SelectObject(oldP);
        memDC.SelectObject(oldBr);

        // 自己：白色圆环高亮
        if (o.id == selfId_) {
            CPen selfPen(PS_SOLID, 2, RGB(255, 255, 255));
            CPen* oldSP = memDC.SelectObject(&selfPen);
            CBrush* oldSB = static_cast<CBrush*>(memDC.SelectStockObject(NULL_BRUSH));
            memDC.Ellipse(sx + 1, sy + 1, sx + cellSize_ - 1, sy + cellSize_ - 1);
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
