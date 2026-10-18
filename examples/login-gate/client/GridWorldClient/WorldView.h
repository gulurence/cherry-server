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
    int size = 1;    // 尺寸（格）：1/2/3
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

    void SetSelf(int64_t selfId, int spawnX, int spawnY, int faction) {
        selfId_ = selfId;
        spawnX_ = spawnX;
        spawnY_ = spawnY;
        viewCx_ = spawnX;
        viewCy_ = spawnY;
        // 把自己也放进对象表，客户端才能画出自己的方块（enter 快照不含自己）。
        WorldObj self;
        self.id = selfId;
        self.kind = 1; // 玩家
        self.faction = faction;
        self.size = 1;
        self.cellX = spawnX;
        self.cellY = spawnY;
        objs_[selfId] = self;
        if (IsWindow(m_hWnd)) Invalidate(FALSE);
    }

    void OnObjPath(int64_t id, int costMs, const std::vector<std::pair<int, int>>& path) {
        paths_[id] = path;
        pathCostMs_[id] = costMs;
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
        paths_.erase(id);
        pathCostMs_.erase(id);
        if (IsWindow(m_hWnd)) Invalidate(FALSE);
    }

    // 动态城堡：服务端下发的阻挡格（仅用于绘制，本地寻路仍只看静态地图）。
    // 进入时用整图 '0'/'1' 串初始化；此后 castleAdd/castleRemove 推送增量改格子。
    void SetCastles(const std::string& s) {
        castleMap_ = s;
        if (IsWindow(m_hWnd)) Invalidate(FALSE);
    }
    void OnCastleAdd(const std::vector<std::pair<int, int>>& cells) {
        for (const auto& c : cells) setCastle(c.first, c.second, true);
        if (IsWindow(m_hWnd)) Invalidate(FALSE);
    }
    void OnCastleRemove(const std::vector<std::pair<int, int>>& cells) {
        for (const auto& c : cells) setCastle(c.first, c.second, false);
        if (IsWindow(m_hWnd)) Invalidate(FALSE);
    }
    // 灯塔视野切换时，服务端补发某个塔的完整城堡快照：先清空该塔区域，再按 cells 重设。
    void OnCastleTower(int tower, const std::vector<std::pair<int, int>>& cells);

    // 鼠标点击移动、视野变化时的回调（由主对话框注入）。
    void SetCallbacks(std::function<void(int, int)> onMoveTo,
                      std::function<void(int, int)> onView) {
        onMoveTo_ = std::move(onMoveTo);
        onView_ = std::move(onView);
    }

    // 客户端本地 A*（与服务器同款 8 方向 Octile）：求 (sx,sy) 到 (gx,gy) 的路径，
    // 不可达返回空。静态方法，无 this 依赖，可在后台线程调用。
    static std::vector<std::pair<int, int>> FindPath(const std::string& map,
                                                     int sx, int sy, int gx, int gy);

    // 自己的当前格子（用于本地寻路起点）。
    std::pair<int, int> GetSelfCell() const {
        auto it = objs_.find(selfId_);
        if (it != objs_.end()) return {it->second.cellX, it->second.cellY};
        return {spawnX_, spawnY_};
    }

    // 地图串（后台线程本地寻路时复制一份）。
    const std::string& GetMap() const { return map_; }

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
    void setCastle(int x, int y, bool blocked);
    CRect minimapRect() const;      // 左上角缩略图在客户区中的矩形
    void drawMinimap(CDC& dc);      // 绘制半透明缩略图（地形 + 对象 + 视野框）

    std::string map_; // 1,000,000 个字符 '0'/'1'/'2'
    int cellSize_ = 30; // 客户端每格 30px（放大 10 倍）
    int64_t selfId_ = 0;
    int spawnX_ = 500;
    int spawnY_ = 500;
    int viewCx_ = 500;
    int viewCy_ = 500;
    std::map<int64_t, WorldObj> objs_;
    std::map<int64_t, std::vector<std::pair<int, int>>> paths_; // 每个对象的路径路点
    std::map<int64_t, int> pathCostMs_;                          // 每个对象最近一次寻路耗时(ms)
    std::string castleMap_; // 1,000,000 个字符 '0'/'1'，'1' 为城堡阻挡格
    std::function<void(int, int)> onMoveTo_;
    std::function<void(int, int)> onView_;
};
