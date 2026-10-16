#pragma once

#include <cstdint>
#include <map>
#include <string>
#include <vector>

// 极简 JSON：只做解析（客户端主要用于解析服务器下发的 JSON），并提供字符串转义。
// 不依赖任何第三方库。
namespace json {

class Value {
public:
    enum Type { Null, Bool, Number, String, Array, Object };

    Type type = Null;
    bool b = false;
    double num = 0.0;
    std::string str;
    std::vector<Value> arr;
    std::map<std::string, Value> obj;

    bool isNull() const { return type == Null; }
    bool isBool() const { return type == Bool; }
    bool isNumber() const { return type == Number; }
    bool isString() const { return type == String; }
    bool isArray() const { return type == Array; }
    bool isObject() const { return type == Object; }

    int asInt() const { return static_cast<int>(num); }
    int64_t asInt64() const { return static_cast<int64_t>(num); }
    double asDouble() const { return num; }
    const std::string& asString() const { return str; }

    bool has(const std::string& key) const {
        return type == Object && obj.find(key) != obj.end();
    }

    // 对象按键查找；不存在或非对象时返回 nullptr。
    const Value* find(const std::string& key) const;

    size_t size() const {
        if (type == Array) return arr.size();
        if (type == Object) return obj.size();
        return 0;
    }

    const Value& operator[](size_t i) const { return arr[i]; }
};

// 解析 JSON 文本；失败返回 Null 并设置 *err（可选）。
Value parse(const std::string& text, std::string* err = nullptr);

// 转义字符串，用于手工拼接请求体（如 {"account":"..."}）。
std::string escape(const std::string& in);

} // namespace json
