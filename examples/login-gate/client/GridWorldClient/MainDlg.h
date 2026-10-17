#pragma once

#include <cstdint>
#include <functional>
#include <map>
#include <memory>
#include <string>

#include "LoginDlg.h"   // EnterInfo
#include "PomeloClient.h"
#include "WorldView.h"

// CMainDlg：登录成功后的主窗口，只承载世界视图。
//
// 连接与进入数据由 CLoginDlg 移交（见构造函数）。此后本窗口负责
// 移动 / 视野请求，以及 objEnter / objMove / objLeave 推送的处理。
class CMainDlg : public CDialogEx {
public:
    CMainDlg(std::unique_ptr<PomeloClient> client, EnterInfo info,
             CWnd* pParent = nullptr);

protected:
    virtual void DoDataExchange(CDataExchange* pDX);
    virtual BOOL OnInitDialog();
    afx_msg void OnDestroy();
    afx_msg void OnSize(UINT nType, int cx, int cy);
    afx_msg LRESULT OnResponse(WPARAM w, LPARAM l);
    afx_msg LRESULT OnPush(WPARAM w, LPARAM l);
    afx_msg LRESULT OnClosed(WPARAM w, LPARAM l);
    DECLARE_MESSAGE_MAP()

private:
    void sendRequest(const std::string& route, const std::string& body,
                     std::function<void(bool, const std::string&)> cb);
    void handleMoveToResp(bool err, const std::string& json);
    void onMoveTo(int x, int y);
    void onView(int cx, int cy);
    void setStatus(const CString& text);

    std::unique_ptr<PomeloClient> client_;
    EnterInfo info_;
    CWorldView world_;
    std::map<uint32_t, std::function<void(bool, const std::string&)>> pending_;
    int64_t selfId_ = 0;
};
