#pragma once

#ifndef VC_EXTRALEAN
#define VC_EXTRALEAN // 从 Windows 头中排除不常用的内容
#endif

#include "targetver.h"

#define _ATL_CSTRING_EXPLICIT_CONSTRUCTORS // 某些 CString 构造函数将是显式的

#include <afxwin.h>    // MFC 核心组件和标准组件
#include <afxext.h>    // MFC 扩展
#include <afxdialogex.h>
#include <afxcmn.h>
