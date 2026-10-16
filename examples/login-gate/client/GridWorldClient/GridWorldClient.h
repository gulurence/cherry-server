#pragma once

// CGridWorldClientApp：MFC 应用程序对象。入口由 MFC 提供，这里只需
// 定义全局 theApp 并在 InitInstance 里创建主对话框。
class CGridWorldClientApp : public CWinApp {
public:
    CGridWorldClientApp() = default;

    virtual BOOL InitInstance();
    virtual int ExitInstance();
};

extern CGridWorldClientApp theApp;
