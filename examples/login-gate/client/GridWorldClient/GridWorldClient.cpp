#include "pch.h"
#include "GridWorldClient.h"
#include "LoginDlg.h"
#include "MainDlg.h"

#ifdef _DEBUG
#define new DEBUG_NEW
#endif

// 唯一的全局应用程序对象。
CGridWorldClientApp theApp;

BOOL CGridWorldClientApp::InitInstance() {
    CWinApp::InitInstance();

    // 初始化 Winsock（pomelo TCP 客户端）。
    WSADATA wsa;
    if (WSAStartup(MAKEWORD(2, 2), &wsa) != 0) {
        return FALSE;
    }

    // 通用控件（对话框 / 控件视觉样式）。
    INITCOMMONCONTROLSEX InitCtrls{};
    InitCtrls.dwSize = sizeof(InitCtrls);
    InitCtrls.dwICC = ICC_WIN95_CLASSES;
    InitCommonControlsEx(&InitCtrls);

    // 先弹登录窗口，登录并进入世界成功后才打开主窗口。
    CLoginDlg loginDlg;
    if (loginDlg.DoModal() != IDOK) {
        return FALSE; // 取消登录，直接退出
    }

    // 移交连接与进入数据，打开世界主窗口。
    CMainDlg dlg(loginDlg.TakeClient(), loginDlg.TakeEnterInfo());
    m_pMainWnd = &dlg;
    dlg.DoModal();

    // 主窗口关闭即退出应用。
    return FALSE;
}

int CGridWorldClientApp::ExitInstance() {
    WSACleanup();
    return CWinApp::ExitInstance();
}
