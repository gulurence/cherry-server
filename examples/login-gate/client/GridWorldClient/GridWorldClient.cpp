#include "pch.h"
#include "GridWorldClient.h"
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

    CMainDlg dlg;
    m_pMainWnd = &dlg;
    dlg.DoModal();

    // DoModal 返回后应用退出（对话框关闭即结束）。
    return FALSE;
}

int CGridWorldClientApp::ExitInstance() {
    WSACleanup();
    return CWinApp::ExitInstance();
}
