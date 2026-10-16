#pragma once

#include <functional>
#include <map>
#include <string>
#include <utility>
#include <vector>

// 世界中的一个对象（玩家 / NPC）。
struct WorldObj {
    int64_t id = 0;
    int kind = 0;    // 0=NPC, 1=玩家
    int faction = 0; // 0红 1黑 2绿
    int cellX = 0;
    int cellY = 0;
};

// CWorldView：负责绘制 1000×1000 格子地图与对象，并处理鼠标点击 / 方向键。
class CWorldView : public CWnd {
public:
    static const int MapW = 1000;
    static const int MapH = 1000;

    CWorldView() = default;

    void SetMap(const std::string& map) {
        map_ = map;
        if (IsWindow(m_hWnd)) Invalidate(FALSE);
    }

    void SetSelf(int64_t selfId, int spawnX, int spawnY) {
        selfId_ = selfId;
        spawnX_ = spawnX;
        spawnY_ = spawnY;
        viewCx_ = spawnX;
        viewCy_ = spawnY;
        if (IsWindow(m_hWnd)) Invalidate(FALSE);
    }

    void SetPath(const std::vector<std::pair<int, int>>& path) {
        ownPath_ = path;
        if (IsWindow(m_hWnd)) Invalidate(FALSE);
    }

    void OnEnterObject(const WorldObj& o) {
        objs_[o.id] = o;
        if (IsWindow(m_hWnd)) Invalidate(FALSE);
    }

    void OnMoveObject(int64_t id, int cellX, int cellY) {
        auto it = objs_.find(id);
        if (it == objs_.end()) return;
        it->second.cellX = cellX;
        it->second.cellY = cellY;
        if (IsWindow(m_hWnd)) Invalidate(FALSE);
    }

    void OnLeaveObject(int64_t id) {
        objs_.erase(id);
        if (IsWindow(m_hWnd)) Invalidate(FALSE);
    }

    // 鼠标点击移动、视野变化时的回调（由主对话框注入）。
    void SetCallbacks(std::function<void(int, int)> onMoveTo,
                      std::function<void(int, int)> onView) {
        onMoveTo_ = std::move(onMoveTo);
        onView_ = std::move(onView);
    }

protected:
    afx_msg void OnPaint();
    afx_msg BOOL OnEraseBkgnd(CDC* pDC);
    afx_msg void OnLButtonDown(UINT nFlags, CPoint point);
    afx_msg void OnKeyDown(UINT nChar, UINT nRepCnt, UINT nFlags);
    afx_msg UINT OnGetDlgCode();
    DECLARE_MESSAGE_MAP()

private:
    void worldToScreen(int cx, int cy, int& sx, int& sy) const;
    void screenToWorld(int sx, int sy, int& cx, int& cy) const;
    COLORREF factionColor(int faction) const;
    int cols() const;
    int rows() const;

    std::string map_; // 1,000,000 个字符 '0'/'1'/'2'
    int cellSize_ = 16;
    int64_t selfId_ = 0;
    int spawnX_ = 500;
    int spawnY_ = 500;
    int viewCx_ = 500;
    int viewCy_ = 500;
    std::map<int64_t, WorldObj> objs_;
    std::vector<std::pair<int, int>> ownPath_;
    std::function<void(int, int)> onMoveTo_;
    std::function<void(int, int)> onView_;
};
