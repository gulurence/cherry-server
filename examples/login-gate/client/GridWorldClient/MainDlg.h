#pragma once

#include <cstdint>
#include <functional>
#include <map>
#include <string>

#include "PomeloClient.h"
#include "WorldView.h"

// CMainDlg：主对话框，承载登录控件（账号/密码/阵营）与 CWorldView。
//
// 所有服务器响应 / 推送都通过 WM_APP_RESPONSE / WM_APP_PUSH / WM_APP_CLOSED
// 投递到 UI 线程处理，避免跨线程访问 MFC 控件。
class CMainDlg : public CDialogEx {
public:
    explicit CMainDlg(CWnd* pParent = nullptr);

protected:
    virtual void DoDataExchange(CDataExchange* pDX);
    virtual BOOL OnInitDialog();
    afx_msg void OnDestroy();
    afx_msg void OnSize(UINT nType, int cx, int cy);
    afx_msg void OnConnect();
    afx_msg LRESULT OnResponse(WPARAM w, LPARAM l);
    afx_msg LRESULT OnPush(WPARAM w, LPARAM l);
    afx_msg LRESULT OnClosed(WPARAM w, LPARAM l);
    DECLARE_MESSAGE_MAP()

private:
    void sendRequest(const std::string& route, const std::string& body,
                     std::function<void(bool, const std::string&)> cb);
    void handleLoginResp(bool err, const std::string& json);
    void handleEnterResp(bool err, const std::string& json);
    void handleMoveToResp(bool err, const std::string& json);
    void onMoveTo(int x, int y);
    void onView(int cx, int cy);
    void setStatus(const CString& text);

    PomeloClient client_;
    CWorldView world_;
    std::map<uint32_t, std::function<void(bool, const std::string&)>> pending_;
    bool entered_ = false;
    int64_t uid_ = 0;
    int64_t selfId_ = 0;
};
