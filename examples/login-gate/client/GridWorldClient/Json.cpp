#include "pch.h"
#include "Json.h"

#include <cctype>
#include <cmath>
#include <cstdio>
#include <cstring>

namespace json {

namespace {

struct Parser {
    const std::string& s;
    size_t i = 0;
    std::string* err;

    Parser(const std::string& t, std::string* e) : s(t), err(e) {}

    void skipWs() {
        while (i < s.size() && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r')) ++i;
    }

    bool fail(const std::string& m) {
        if (err) *err = m;
        return false;
    }

    bool match(const char* lit) {
        size_t n = std::strlen(lit);
        if (s.compare(i, n, lit) == 0) {
            i += n;
            return true;
        }
        return false;
    }

    bool parseValue(Value& out) {
        skipWs();
        if (i >= s.size()) return fail("unexpected end");
        char c = s[i];
        if (c == '{') return parseObject(out);
        if (c == '[') return parseArray(out);
        if (c == '"') {
            out.type = Value::String;
            return parseString(out.str);
        }
        if (c == 't') {
            if (match("true")) { out.type = Value::Bool; out.b = true; return true; }
            return fail("bad true");
        }
        if (c == 'f') {
            if (match("false")) { out.type = Value::Bool; out.b = false; return true; }
            return fail("bad false");
        }
        if (c == 'n') {
            if (match("null")) { out.type = Value::Null; return true; }
            return fail("bad null");
        }
        if (c == '-' || (c >= '0' && c <= '9')) {
            out.type = Value::Number;
            return parseNumber(out.num);
        }
        return fail("unexpected char");
    }

    bool parseObject(Value& out) {
        out.type = Value::Object;
        ++i; // {
        skipWs();
        if (i < s.size() && s[i] == '}') { ++i; return true; }
        while (true) {
            skipWs();
            if (i >= s.size() || s[i] != '"') return fail("expect key");
            std::string key;
            if (!parseString(key)) return false;
            skipWs();
            if (i >= s.size() || s[i] != ':') return fail("expect :");
            ++i;
            Value v;
            if (!parseValue(v)) return false;
            out.obj[key] = std::move(v);
            skipWs();
            if (i < s.size() && s[i] == ',') { ++i; continue; }
            if (i < s.size() && s[i] == '}') { ++i; return true; }
            return fail("expect , or }");
        }
    }

    bool parseArray(Value& out) {
        out.type = Value::Array;
        ++i; // [
        skipWs();
        if (i < s.size() && s[i] == ']') { ++i; return true; }
        while (true) {
            Value v;
            if (!parseValue(v)) return false;
            out.arr.push_back(std::move(v));
            skipWs();
            if (i < s.size() && s[i] == ',') { ++i; continue; }
            if (i < s.size() && s[i] == ']') { ++i; return true; }
            return fail("expect , or ]");
        }
    }

    bool parseString(std::string& out) {
        if (i >= s.size() || s[i] != '"') return fail("expect \"");
        ++i;
        out.clear();
        while (i < s.size()) {
            char c = s[i++];
            if (c == '"') return true;
            if (c == '\\') {
                if (i >= s.size()) return fail("bad escape");
                char e = s[i++];
                switch (e) {
                    case '"': out += '"'; break;
                    case '\\': out += '\\'; break;
                    case '/': out += '/'; break;
                    case 'b': out += '\b'; break;
                    case 'f': out += '\f'; break;
                    case 'n': out += '\n'; break;
                    case 'r': out += '\r'; break;
                    case 't': out += '\t'; break;
                    case 'u': {
                        if (i + 4 > s.size()) return fail("bad unicode");
                        unsigned cp = 0;
                        for (int k = 0; k < 4; ++k) {
                            char h = s[i++];
                            cp <<= 4;
                            if (h >= '0' && h <= '9') cp |= (h - '0');
                            else if (h >= 'a' && h <= 'f') cp |= (h - 'a' + 10);
                            else if (h >= 'A' && h <= 'F') cp |= (h - 'A' + 10);
                            else return fail("bad hex");
                        }
                        if (cp < 0x80) {
                            out += static_cast<char>(cp);
                        } else if (cp < 0x800) {
                            out += static_cast<char>(0xC0 | (cp >> 6));
                            out += static_cast<char>(0x80 | (cp & 0x3F));
                        } else {
                            out += static_cast<char>(0xE0 | (cp >> 12));
                            out += static_cast<char>(0x80 | ((cp >> 6) & 0x3F));
                            out += static_cast<char>(0x80 | (cp & 0x3F));
                        }
                        break;
                    }
                    default:
                        return fail("bad escape char");
                }
            } else {
                out += c;
            }
        }
        return fail("unterminated string");
    }

    bool parseNumber(double& out) {
        size_t start = i;
        if (i < s.size() && s[i] == '-') ++i;
        while (i < s.size() && std::isdigit(static_cast<unsigned char>(s[i]))) ++i;
        if (i < s.size() && s[i] == '.') {
            ++i;
            while (i < s.size() && std::isdigit(static_cast<unsigned char>(s[i]))) ++i;
        }
        if (i < s.size() && (s[i] == 'e' || s[i] == 'E')) {
            ++i;
            if (i < s.size() && (s[i] == '+' || s[i] == '-')) ++i;
            while (i < s.size() && std::isdigit(static_cast<unsigned char>(s[i]))) ++i;
        }
        out = std::strtod(s.substr(start, i - start).c_str(), nullptr);
        return true;
    }
};

} // namespace

const Value* Value::find(const std::string& key) const {
    if (type != Object) return nullptr;
    auto it = obj.find(key);
    return it == obj.end() ? nullptr : &it->second;
}

Value parse(const std::string& text, std::string* err) {
    Parser p(text, err);
    Value v;
    if (!p.parseValue(v)) {
        if (err && err->empty()) *err = "parse error";
        return Value{};
    }
    return v;
}

std::string escape(const std::string& in) {
    std::string out;
    out.reserve(in.size() + 8);
    for (char c : in) {
        switch (c) {
            case '"': out += "\\\""; break;
            case '\\': out += "\\\\"; break;
            case '\n': out += "\\n"; break;
            case '\r': out += "\\r"; break;
            case '\t': out += "\\t"; break;
            default:
                if (static_cast<unsigned char>(c) < 0x20) {
                    char b[8];
                    std::snprintf(b, sizeof(b), "\\u%04x", static_cast<unsigned char>(c));
                    out += b;
                } else {
                    out += c;
                }
        }
    }
    return out;
}

} // namespace json
