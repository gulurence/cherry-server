#pragma once

#include <cstdint>
#include <functional>
#include <map>
#include <memory>
#include <string>
#include <vector>

#include "PomeloClient.h"
#include "WorldView.h"

// EnterInfo：登录并进入世界成功后，从登录窗口移交给主窗口的数据。
struct EnterInfo {
    std::string map;               // 1,000,000 个字符 '0'/'1'/'2'
    int64_t selfId = 0;            // 自己的对象 id
    int spawnX = 0;
    int spawnY = 0;
    int faction = 0;
    std::vector<WorldObj> objects; // 初始可见对象快照
    std::string castles;           // 1,000,000 个字符 '0'/'1'（仅绘制，本地寻路只看静态地图）
};

// CLoginDlg：独立的登录窗口，负责连接 + 登录 + 进入世界。
//
// 成功后关闭自身，调用方通过 TakeClient() / TakeEnterInfo() 取走
// 连接与进入数据，再打开主窗口。连接只在登录失败/取消时才会在此处断开。
class CLoginDlg : public CDialogEx {
public:
    explicit CLoginDlg(CWnd* pParent = nullptr);

    std::unique_ptr<PomeloClient> TakeClient() { return std::move(client_); }
    EnterInfo TakeEnterInfo() { return std::move(info_); }

protected:
    virtual void DoDataExchange(CDataExchange* pDX);
    virtual BOOL OnInitDialog();
    afx_msg void OnDestroy();
    afx_msg void OnConnect();
    afx_msg LRESULT OnResponse(WPARAM w, LPARAM l);
    afx_msg LRESULT OnClosed(WPARAM w, LPARAM l);
    DECLARE_MESSAGE_MAP()

private:
    void sendRequest(const std::string& route, const std::string& body,
                     std::function<void(bool, const std::string&)> cb);
    void handleLoginResp(bool err, const std::string& json);
    void handleEnterResp(bool err, const std::string& json);
    void resetConnection();
    void setStatus(const CString& text);

    std::unique_ptr<PomeloClient> client_;
    std::map<uint32_t, std::function<void(bool, const std::string&)>> pending_;
    EnterInfo info_;
    bool success_ = false; // 进入成功，连接将移交给主窗口，不再在此断开
};
