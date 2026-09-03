#pragma once
#include <algorithm>
#include <cmath>
#include <cstdint>
#include <limits>
#include <numeric>
#include <optional>
#include <string>
#include <variant>
#include <vector>

namespace r2cpp {

struct NA_t {};
inline constexpr NA_t NA{};

using Logical = std::optional<bool>;
using Atomic = std::variant<NA_t,bool,std::int64_t,double,std::string>;
using Vector = std::vector<Atomic>;

template<class T> inline std::size_t length(const std::vector<T>& x){ return x.size(); }
template<class T> inline T sum(const std::vector<T>& x){ return std::accumulate(x.begin(),x.end(),T{}); }
template<class T> inline double mean(const std::vector<T>& x){
    if(x.empty()) return std::numeric_limits<double>::quiet_NaN();
    return static_cast<double>(sum(x))/static_cast<double>(x.size());
}
template<class T> inline T min(const std::vector<T>& x){ return *std::min_element(x.begin(),x.end()); }
template<class T> inline T max(const std::vector<T>& x){ return *std::max_element(x.begin(),x.end()); }
template<class T> inline std::vector<T> sort(std::vector<T> x){ std::sort(x.begin(),x.end()); return x; }

template<class T> inline std::vector<T> cumsum(const std::vector<T>& x){
    std::vector<T> out; out.reserve(x.size()); T acc{};
    for(const auto& v:x){ acc+=v; out.push_back(acc); } return out;
}
template<class T> inline std::vector<T> cumprod(const std::vector<T>& x){
    std::vector<T> out; out.reserve(x.size()); T acc=static_cast<T>(1);
    for(const auto& v:x){ acc*=v; out.push_back(acc); } return out;
}

} // namespace r2cpp
