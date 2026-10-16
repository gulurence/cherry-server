#pragma once

// winsock2.h 必须在 afxwin.h（内部包含 windows.h -> winsock.h）之前包含，
// 否则会出现 winsock 头冲突。winsock2.h 定义 _WINSOCKAPI_ 使 winsock.h 被跳过。
#include <winsock2.h>
#include <ws2tcpip.h>

#include "framework.h"

// 通用 STL 头，减少各文件重复包含。
#include <atomic>
#include <cstdint>
#include <functional>
#include <map>
#include <memory>
#include <string>
#include <thread>
#include <vector>
