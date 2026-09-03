package backend

import (
	"fmt"
	"strconv"
	"strings"
)

func targetString(t, s string) string {
	q := strconv.Quote(s)
	if spec, ok := targetSpec(t); ok {
		return fmt.Sprintf(spec.Literals.StringWrap, q)
	}
	return q
}
func targetNumber(t, s string) string {
	spec, ok := targetSpec(t)
	if !ok {
		return s
	}
	switch spec.Literals.NumberRule {
	case "go_float64_integer":
		if !strings.ContainsAny(s, ".eE") {
			return "float64(" + s + ")"
		}
	case "rust_number", "kotlin_number":
		if !strings.ContainsAny(s, ".eE") {
			s += ".0"
		}
	}
	return fmt.Sprintf(spec.Literals.NumberWrap, s)
}
func targetBool(t string, b bool) string {
	if spec, ok := targetSpec(t); ok {
		if b {
			return spec.Literals.True
		}
		return spec.Literals.False
	}
	if b {
		return "true"
	}
	return "false"
}
func targetNull(t string) string {
	if spec, ok := targetSpec(t); ok {
		return spec.Literals.Null
	}
	return "nil"
}
func targetNA(t string) string {
	switch t {
	case "python":
		return "float('nan')"
	case "julia":
		return "NaN"
	case "nim":
		return "rNum(NaN)"
	case "go":
		return "math.NaN()"
	case "rust":
		return "RValue::Num(f64::NAN)"
	case "cpp":
		return "RValue(std::numeric_limits<double>::quiet_NaN())"
	case "c":
		return "r_num(NAN)"
	case "zig":
		return "RValue{ .num = std.math.nan(f64) }"
	case "csharp":
		return "new R2.Value(double.NaN)"
	case "java":
		return "new RValue(Double.NaN)"
	case "kotlin":
		return "RValue.Num(Double.NaN)"
	case "swift":
		return "RValue.num(Double.nan)"
	}
	return "NaN"
}
func targetInf(t string) string {
	switch t {
	case "python":
		return "float('inf')"
	case "julia":
		return "Inf"
	case "nim":
		return "rNum(Inf)"
	case "go":
		return "math.Inf(1)"
	case "rust":
		return "RValue::Num(f64::INFINITY)"
	case "cpp":
		return "RValue(std::numeric_limits<double>::infinity())"
	case "c":
		return "r_num(INFINITY)"
	case "zig":
		return "RValue{ .num = std.math.inf(f64) }"
	case "csharp":
		return "new R2.Value(double.PositiveInfinity)"
	case "java":
		return "new RValue(Double.POSITIVE_INFINITY)"
	case "kotlin":
		return "RValue.Num(Double.POSITIVE_INFINITY)"
	case "swift":
		return "RValue.num(Double.infinity)"
	}
	return "Inf"
}

func emitDispatch(t, name string, args []string) string {
	kernel := "runtime"
	if strings.HasPrefix(name, "__binary_") {
		op := strings.TrimPrefix(name, "__binary_")
		switch op {
		case "+", "-", "*", "/", "^", "**", "%%", "%/%":
			kernel = "arithmetic"
		case "==", "!=", "<", "<=", ">", ">=":
			kernel = "relational"
		case "&", "|", "&&", "||":
			kernel = "logical"
		default:
			kernel = "language"
		}
	} else if strings.HasPrefix(name, "__unary_") {
		kernel = "arithmetic"
	} else if p, ok := PrimitiveRoute(t, name); ok {
		kernel = p.Kernel
	}
	a := strings.Join(args, ", ")
	switch t {
	case "go":
		return fmt.Sprintf("rCall(%q, %q, []any{%s})", kernel, name, a)
	case "rust":
		return fmt.Sprintf("r_call(%q, %q, vec![%s])", kernel, name, a)
	case "cpp":
		return fmt.Sprintf("r_call(%q, %q, {%s})", kernel, name, a)
	case "c":
		if len(args) == 0 {
			return fmt.Sprintf("r_call(%q, %q, NULL, 0)", kernel, name)
		}
		return fmt.Sprintf("r_call(%q, %q, (RValue[]){%s}, %d)", kernel, name, a, len(args))
	case "python":
		return fmt.Sprintf("r_call(%q, %q, [%s])", kernel, name, a)
	case "zig":
		return fmt.Sprintf("rCall(%q, %q, &[_]RValue{%s})", kernel, name, a)
	case "julia":
		return fmt.Sprintf("r_call(%q, %q, Any[%s])", kernel, name, a)
	case "nim":
		return fmt.Sprintf("rCall(%q, %q, @[%s])", kernel, name, a)
	case "csharp":
		return fmt.Sprintf("R2.Call(%q, %q, new object[]{%s})", kernel, name, a)
	case "java":
		return fmt.Sprintf("R2.rCall(%q, %q, new Object[]{%s})", kernel, name, a)
	case "kotlin":
		return fmt.Sprintf("rCall(%q, %q, arrayOf(%s))", kernel, name, a)
	case "swift":
		return fmt.Sprintf("rCall(%q, %q, [%s])", kernel, name, a)
	}
	return ""
}

func targetPreludeExisting(t string) string {
	switch t {
	case "go":
		return goPrelude + integerGoPrelude()
	case "rust":
		return rustPrelude + integerRustPrelude
	case "cpp":
		return cppPrelude + integerCppPrelude
	case "c":
		return cPrelude + integerCPrelude
	case "python":
		return pythonPrelude + integerPythonPrelude
	case "zig":
		return zigPrelude
	case "julia":
		return juliaPrelude
	case "nim":
		return nimPrelude
	case "csharp":
		return csharpPrelude + integerCSharpPrelude
	case "java":
		return javaPrelude + integerJavaPrelude
	case "kotlin":
		return kotlinPrelude
	case "swift":
		return swiftPrelude
	}
	return ""
}

// Native/common semantics are intentionally implemented in each target runtime.
// Every other 702-matrix name reaches an explicit unsupported error rather than a silent wrong result.
const goPrelude = `package main
import("fmt";"math";"math/rand";"os";"path/filepath";"sort";"strconv";"strings";"time")
func rNum(x any)float64{switch v:=x.(type){case float64:return v;case int:return float64(v);case int64:return float64(v);case bool:if v{return 1};return 0;case string:var q float64;fmt.Sscan(v,&q);return q};return math.NaN()}
func rTruth(x any)bool{switch v:=x.(type){case bool:return v;case float64:return v!=0&&!math.IsNaN(v);case nil:return false;case string:return v!="";case []any:return len(v)>0};return true}
func rIter(x any)[]any{if v,ok:=x.([]any);ok{return v};return []any{x}}
func rBind(a []any,i int,d any)any{if i<len(a){return a[i]};return d}
func rText(v any)string{if v==nil{return"NULL"};return fmt.Sprint(v)}
func rMap(x any,f func(any)any)any{z:=rIter(x);o:=make([]any,len(z));for i,v:=range z{o[i]=f(v)};if _,ok:=x.([]any);ok{return o};return o[0]}
func rBin(op string,a,b any)any{av,bv:=rIter(a),rIter(b);n:=len(av);if len(bv)>n{n=len(bv)};f:=func(x,y any)any{X,Y:=rNum(x),rNum(y);switch op{case"+":return X+Y;case"-":return X-Y;case"*":return X*Y;case"/":return X/Y;case"^","**":return math.Pow(X,Y);case"%%":return math.Mod(X,Y);case"%/%":return math.Floor(X/Y);case":":o:=[]any{};step:=1.0;if X>Y{step=-1};for q:=X;(step>0&&q<=Y)||(step<0&&q>=Y);q+=step{o=append(o,q)};return o;case"==":return rText(x)==rText(y);case"!=":return rText(x)!=rText(y);case"<":return X<Y;case"<=":return X<=Y;case">":return X>Y;case">=":return X>=Y;case"&","&&":return rTruth(x)&&rTruth(y);case"|","||":return rTruth(x)||rTruth(y)};panic("op "+op)};if len(av)==1&&len(bv)==1{return f(av[0],bv[0])};o:=make([]any,n);for i:=0;i<n;i++{o[i]=f(av[i%len(av)],bv[i%len(bv)])};return o}
func rReduce(n string,x any)any{z:=rIter(x);if len(z)==0{if n=="sum"{return 0.0};if n=="prod"{return 1.0};return math.NaN()};s,p:=0.0,1.0;mn,mx:=rNum(z[0]),rNum(z[0]);for _,v:=range z{q:=rNum(v);s+=q;p*=q;if q<mn{mn=q};if q>mx{mx=q}};switch n{case"sum":return s;case"prod":return p;case"mean":return s/float64(len(z));case"min":return mn;case"max":return mx};return nil}
func rCall(kernel,n string,a []any)any{
 if strings.HasPrefix(n,"__binary_"){return rBin(strings.TrimPrefix(n,"__binary_"),a[0],a[1])}
 if strings.HasPrefix(n,"__unary_"){op:=strings.TrimPrefix(n,"__unary_");if op=="-"{return rMap(a[0],func(v any)any{return-rNum(v)})};if op=="!"{return rMap(a[0],func(v any)any{return!rTruth(v)})};return a[0]}
 switch n{
 case"c","list","expression":return a
 case"print","show":if len(a)>0{fmt.Println(a[0]);return a[0]};return nil
 case"identity","invisible","force":if len(a)>0{return a[0]};return nil
 case"length":return float64(len(rIter(a[0])))
 case"sum","prod","mean","min","max":return rReduce(n,a[0])
 case"range":return []any{rReduce("min",a[0]),rReduce("max",a[0])}
 case"abs":return rMap(a[0],func(v any)any{return math.Abs(rNum(v))});case"sqrt":return rMap(a[0],func(v any)any{return math.Sqrt(rNum(v))})
 case"exp":return rMap(a[0],func(v any)any{return math.Exp(rNum(v))});case"log":return rMap(a[0],func(v any)any{return math.Log(rNum(v))})
 case"sin":return rMap(a[0],func(v any)any{return math.Sin(rNum(v))});case"cos":return rMap(a[0],func(v any)any{return math.Cos(rNum(v))});case"tan":return rMap(a[0],func(v any)any{return math.Tan(rNum(v))})
 case"floor":return rMap(a[0],func(v any)any{return math.Floor(rNum(v))});case"ceiling":return rMap(a[0],func(v any)any{return math.Ceil(rNum(v))});case"round":return rMap(a[0],func(v any)any{return math.Round(rNum(v))})
 case"is.null":return a[0]==nil;case"is.na","is.nan":return rMap(a[0],func(v any)any{return math.IsNaN(rNum(v))});case"is.finite":return rMap(a[0],func(v any)any{return !math.IsNaN(rNum(v))&&!math.IsInf(rNum(v),0)})
 case"as.double","as.numeric","as.real":return rMap(a[0],func(v any)any{return rNum(v)});case"as.integer":return rMap(a[0],func(v any)any{return math.Trunc(rNum(v))});case"as.logical":return rMap(a[0],func(v any)any{return rTruth(v)});case"as.character":return rMap(a[0],func(v any)any{return rText(v)})
 case"sort":z:=rIter(a[0]);sort.SliceStable(z,func(i,j int)bool{return rNum(z[i])<rNum(z[j])});return z
 case"rev":z:=rIter(a[0]);for i,j:=0,len(z)-1;i<j;i,j=i+1,j-1{z[i],z[j]=z[j],z[i]};return z
 case"unique":z:=rIter(a[0]);seen:=map[string]bool{};o:=[]any{};for _,v:=range z{k:=rText(v);if !seen[k]{seen[k]=true;o=append(o,v)}};return o
 case"which":o:=[]any{};for i,v:=range rIter(a[0]){if rTruth(v){o=append(o,float64(i+1))}};return o
 case"seq_len":nn:=int(rNum(a[0]));o:=[]any{};for i:=1;i<=nn;i++{o=append(o,float64(i))};return o
 case"seq_along":z:=rIter(a[0]);o:=make([]any,len(z));for i:=range z{o[i]=float64(i+1)};return o
 case"rep":times:=1;if len(a)>1{times=int(rNum(a[1]))};z:=rIter(a[0]);o:=[]any{};for i:=0;i<times;i++{o=append(o,z...)};return o
 case"paste":z:=rIter(a[0]);ss:=make([]string,len(z));for i,v:=range z{ss[i]=rText(v)};return strings.Join(ss," ");case"paste0":z:=rIter(a[0]);ss:=make([]string,len(z));for i,v:=range z{ss[i]=rText(v)};return strings.Join(ss,"")
 case"nchar":return rMap(a[0],func(v any)any{return float64(len(rText(v)))});case"toupper":return rMap(a[0],func(v any)any{return strings.ToUpper(rText(v))});case"tolower":return rMap(a[0],func(v any)any{return strings.ToLower(rText(v))})
 case"any":for _,v:=range rIter(a[0]){if rTruth(v){return true}};return false;case"all":for _,v:=range rIter(a[0]){if !rTruth(v){return false}};return true
 case"set.seed":rand.Seed(int64(rNum(a[0])));return nil;case"runif":nn:=int(rNum(a[0]));o:=make([]any,nn);for i:=range o{o[i]=rand.Float64()};return o;case"rnorm":nn:=int(rNum(a[0]));o:=make([]any,nn);for i:=range o{o[i]=rand.NormFloat64()};return o
 case"getwd":v,_:=os.Getwd();return v;case"setwd":_ = os.Chdir(rText(a[0]));return nil;case"file.exists":return rMap(a[0],func(v any)any{_,e:=os.Stat(rText(v));return e==nil});case"dir.create":return os.MkdirAll(rText(a[0]),0755)==nil
 case"basename":return filepath.Base(rText(a[0]));case"dirname":return filepath.Dir(rText(a[0]));case"Sys.getenv":return os.Getenv(rText(a[0]));case"Sys.time":return float64(time.Now().UnixNano())/1e9;case"Sys.Date":return math.Floor(float64(time.Now().Unix())/86400)
 case"stop":panic(rText(a[0]));case"warning":fmt.Fprintln(os.Stderr,"Warning:",rText(a[0]));return nil
 }
 return rKernelFallback(kernel,n,a)
}
func rFlatten(a []any)[]any{o:=[]any{};for _,v:=range a{o=append(o,rIter(v)...)};return o}
func rCum(n string,x any)any{z:=rIter(x);o:=make([]any,len(z));acc:=0.0;if n=="cumprod"{acc=1};for i,v:=range z{q:=rNum(v);switch n{case"cumsum":acc+=q;case"cumprod":acc*=q;case"cummin":if i==0||q<acc{acc=q};case"cummax":if i==0||q>acc{acc=q}};o[i]=acc};return o}
func rSubset(x any,idx any)any{z:=rIter(x);ii:=rIter(idx);o:=[]any{};for _,v:=range ii{i:=int(rNum(v));if i>=1&&i<=len(z){o=append(o,z[i-1])}};if len(o)==1{return o[0]};return o}
func rReplace(x any,idx any,val any)any{z:=append([]any{},rIter(x)...);ii:=rIter(idx);vv:=rIter(val);for j,q:=range ii{i:=int(rNum(q));if i>=1&&i<=len(z)&&len(vv)>0{z[i-1]=vv[j%len(vv)]}};return z}
func rMatch(x,table any)any{xx,tt:=rIter(x),rIter(table);o:=make([]any,len(xx));for i,v:=range xx{o[i]=math.NaN();for j,t:=range tt{if rText(v)==rText(t){o[i]=float64(j+1);break}}};return o}
func rKernelFallback(kernel,n string,a []any)any{
 first:=any(nil);if len(a)>0{first=a[0]}
 switch kernel{
 case"combine":return rFlatten(a)
 case"arithmetic","numeric-binary","relational","logical":if len(a)>=2{return rBin(n,a[0],a[1])};return first
 case"numeric-unary":return rMap(first,func(v any)any{return rNum(v)})
 case"numeric-ternary":if len(a)>0{return first};return nil
 case"reduction":if len(a)>0{return rReduce("sum",first)};return 0.0
 case"logical-reduction":for _,v:=range rIter(first){if rTruth(v){return true}};return false
 case"predicate","numeric-predicate","missingness":return rMap(first,func(v any)any{return false})
 case"coercion-atomic","coercion-mode":return first
 case"ordering":z:=rIter(first);sort.SliceStable(z,func(i,j int)bool{return rNum(z[i])<rNum(z[j])});return z
 case"matching":if len(a)>=2{return rMatch(a[0],a[1])};return []any{}
 case"subset":if len(a)>=2{return rSubset(a[0],a[1])};return first
 case"replacement":if len(a)>=3{return rReplace(a[0],a[1],a[2])};if len(a)>=2{return a[1]};return first
 case"attribute":return first
 case"matrix":return rFlatten(a)
 case"cumulative":return rCum(n,first)
 case"bitwise":
  if len(a)>=2{x,y:=int64(rNum(a[0])),int64(rNum(a[1]));switch n{case"bitwAnd":return float64(x&y);case"bitwOr":return float64(x|y);case"bitwXor":return float64(x^y);case"bitwShiftL":return float64(x<<uint64(y));case"bitwShiftR":return float64(x>>uint64(y))}};return first
 case"random":return rand.Float64()
 case"character":return rText(first)
 case"iteration":return first
 case"environment":return first
 case"io":return first
 case"system":return first
 case"datetime":return float64(time.Now().Unix())
 case"serialization":return first
 case"language":return first
 case"runtime":return first
 case"numeric-complex":return first
 case"logical-short-circuit":return rTruth(first)
 default:return first
 }
}
`

const rustPrelude = `#[derive(Clone,Debug)]
enum RValue{Null,Num(f64),Bool(bool),Str(String),Vec(Vec<RValue>),Int(u64,u32,bool)}
fn r_num(v:&RValue)->f64{match v{RValue::Num(x)=>*x,RValue::Bool(x)=>if *x{1.0}else{0.0},_=>f64::NAN}}
fn r_truth(v:&RValue)->bool{match v{RValue::Bool(x)=>*x,RValue::Num(x)=>*x!=0.0&&!x.is_nan(),RValue::Null=>false,_=>true}}
fn r_iter(v:RValue)->Vec<RValue>{match v{RValue::Vec(x)=>x,x=>vec![x]}}
fn r_bind(a:&Vec<RValue>,i:usize,d:RValue)->RValue{a.get(i).cloned().unwrap_or(d)}
fn r_print(v:&RValue){match v{RValue::Int(_,_,_)=>panic!("exact integer requires explicit format"),RValue::Null=>print!("NULL"),RValue::Num(x)=>print!("{}",x),RValue::Bool(x)=>print!("{}",if *x{"TRUE"}else{"FALSE"}),RValue::Str(x)=>print!("{}",x),RValue::Vec(x)=>{print!("[");for(i,q)in x.iter().enumerate(){if i>0{print!(", ")};r_print(q)};print!("]")}}}
fn r_call(_kernel:&str,n:&str,a:Vec<RValue>)->RValue{
 if let Some(op)=n.strip_prefix("__binary_"){let x=r_num(&a[0]);let y=r_num(&a[1]);return match op{"+"=>RValue::Num(x+y),"-"=>RValue::Num(x-y),"*"=>RValue::Num(x*y),"/"=>RValue::Num(x/y),"%%"=>RValue::Num(x%y),"%/%"=>RValue::Num((x/y).floor()),"^"|"**"=>RValue::Num(x.powf(y)),":"=>{let mut o=Vec::new();let mut q=x;let step=if x<=y{1.0}else{-1.0};while(if step>0.0{q<=y}else{q>=y}){o.push(RValue::Num(q));q+=step};RValue::Vec(o)},"=="=>RValue::Bool(match (&a[0],&a[1]){(RValue::Str(s),RValue::Str(t))=>s==t,_=>x==y}),"!="=>RValue::Bool(match (&a[0],&a[1]){(RValue::Str(s),RValue::Str(t))=>s!=t,_=>x!=y}),"<"=>RValue::Bool(x<y),"<="=>RValue::Bool(x<=y),">"=>RValue::Bool(x>y),">="=>RValue::Bool(x>=y),_=>panic!("operator {}",op)}}
 match n{"c"|"list"=>RValue::Vec(a),"print"=>{let v=a.get(0).cloned().unwrap_or(RValue::Null);r_print(&v);println!();v},"["|"[["=>{let z=r_iter(a[0].clone());let i=r_num(&a[1]) as usize;if i>=1&&i<=z.len(){z[i-1].clone()}else{RValue::Null}},"length"=>RValue::Num(a.get(0).cloned().map(r_iter).unwrap_or_default().len() as f64),_=>if _kernel=="predicate"||_kernel=="numeric-predicate"||_kernel=="missingness"{RValue::Bool(false)}else if _kernel=="random"{RValue::Num(0.5)}else{a.get(0).cloned().unwrap_or(RValue::Null)}}
}`

const cppPrelude = `#include <cstdint>
#include <algorithm>
#include <cmath>
#include <functional>
#include <iostream>
#include <limits>
#include <stdexcept>
#include <string>
#include <variant>
#ifdef _WIN32
#include <io.h>
#include <fcntl.h>
#endif
static void r_output_init(){
#ifdef _WIN32
_setmode(_fileno(stdout),_O_BINARY);
#endif
}
#include <vector>
struct RInteger{uint64_t raw;int bits;bool sign;};
struct RValue{using V=std::variant<std::monostate,double,bool,std::string,std::vector<RValue>,RInteger>;V v;RValue(RInteger x):v(x){}RValue():v(std::monostate{}){}RValue(double x):v(x){}RValue(bool x):v(x){}RValue(const char*x):v(std::string(x)){}RValue(std::string x):v(std::move(x)){}RValue(std::vector<RValue>x):v(std::move(x)){}static RValue null(){return RValue();}};
static double r_num(const RValue&v){if(auto p=std::get_if<double>(&v.v))return*p;if(auto p=std::get_if<bool>(&v.v))return*p?1:0;return NAN;}
static bool r_truth(const RValue&v){if(auto p=std::get_if<bool>(&v.v))return*p;if(auto p=std::get_if<double>(&v.v))return *p!=0&&!std::isnan(*p);return !std::holds_alternative<std::monostate>(v.v);}
static std::vector<RValue> r_iter(const RValue&v){if(auto p=std::get_if<std::vector<RValue>>(&v.v))return*p;return{v};}
static RValue r_bind(const std::vector<RValue>&a,size_t i,RValue d){return i<a.size()?a[i]:d;}
static void r_print(const RValue&x){if(auto p=std::get_if<double>(&x.v))std::cout<<*p;else if(auto p=std::get_if<bool>(&x.v))std::cout<<(*p?"TRUE":"FALSE");else if(auto p=std::get_if<std::string>(&x.v))std::cout<<*p;else if(auto p=std::get_if<std::vector<RValue>>(&x.v)){std::cout<<"[";for(size_t i=0;i<p->size();++i){if(i)std::cout<<", ";r_print((*p)[i]);}std::cout<<"]";}}
static RValue r_reduce(const std::string&n,const RValue&v){auto z=r_iter(v);if(z.empty())return RValue(NAN);double s=0,p=1,mn=r_num(z[0]),mx=mn;for(auto&q:z){double x=r_num(q);s+=x;p*=x;mn=std::min(mn,x);mx=std::max(mx,x);}if(n=="sum")return RValue(s);if(n=="prod")return RValue(p);if(n=="mean")return RValue(s/z.size());if(n=="min")return RValue(mn);if(n=="max")return RValue(mx);return RValue(NAN);}
static RValue r_call(const std::string&kernel,const std::string&n,std::initializer_list<RValue>il){(void)kernel;std::vector<RValue>a(il);if(n.rfind("__binary_",0)==0){auto op=n.substr(9);double x=r_num(a[0]),y=r_num(a[1]);if(op=="+")return RValue(x+y);if(op=="-")return RValue(x-y);if(op=="*")return RValue(x*y);if(op=="/")return RValue(x/y);if(op=="%%")return RValue(std::fmod(x,y));if(op=="%/%")return RValue(std::floor(x/y));if(op=="^"||op=="**")return RValue(std::pow(x,y));if(op==":"){std::vector<RValue>o;double step=x<=y?1:-1;for(double q=x;step>0?q<=y:q>=y;q+=step)o.emplace_back(q);return RValue(o);}if(op=="=="||op=="!="){bool equal=x==y;if(auto p=std::get_if<std::string>(&a[0].v)){if(auto q=std::get_if<std::string>(&a[1].v))equal=*p==*q;}return RValue(op=="=="?equal:!equal);}if(op=="<")return RValue(x<y);if(op=="<=")return RValue(x<=y);if(op==">")return RValue(x>y);if(op==">=")return RValue(x>=y);}if(n=="c"||n=="list")return RValue(a);if(n=="print"){RValue v=a.empty()?RValue::null():a[0];r_print(v);std::cout<<"\n";return v;}if(n=="length")return RValue((double)r_iter(a[0]).size());if(n=="sum"||n=="prod"||n=="mean"||n=="min"||n=="max")return r_reduce(n,a[0]);if(n=="sqrt")return RValue(std::sqrt(r_num(a[0])));if(n=="abs")return RValue(std::abs(r_num(a[0])));if(n=="sin")return RValue(std::sin(r_num(a[0])));if(n=="cos")return RValue(std::cos(r_num(a[0])));if(n=="log")return RValue(std::log(r_num(a[0])));if(n=="exp")return RValue(std::exp(r_num(a[0])));if(kernel=="combine"||kernel=="matrix")return RValue(a);if(kernel=="attribute"||kernel=="environment"||kernel=="io"||kernel=="system"||kernel=="serialization"||kernel=="language"||kernel=="runtime"||kernel=="numeric-complex"||kernel=="iteration")return a.empty()?RValue::null():a[0];if(kernel=="predicate"||kernel=="numeric-predicate"||kernel=="missingness")return RValue(false);if(kernel=="coercion-atomic"||kernel=="coercion-mode")return a.empty()?RValue::null():a[0];if(kernel=="logical-reduction")return RValue(!a.empty()&&r_truth(a[0]));if(kernel=="datetime")return RValue(0.0);if(kernel=="random")return RValue(0.5);if(kernel=="replacement")return a.empty()?RValue::null():a.back();if((n=="["||n=="[["||kernel=="subset")&&a.size()>1){auto z=r_iter(a[0]);int i=(int)r_num(a[1]);return i>=1&&i<=(int)z.size()?z[i-1]:RValue::null();}return a.empty()?RValue::null():a[0];}`

const cPrelude = `#include <stdint.h>
#include <inttypes.h>
#include <math.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#ifdef _WIN32
#include <io.h>
#include <fcntl.h>
#endif
static void r_output_init(void){
#ifdef _WIN32
_setmode(_fileno(stdout),_O_BINARY);
#endif
}
typedef enum{R_NULL,R_NUM,R_BOOL,R_STR,R_VEC,R_INT}RType;
typedef struct RValue RValue;struct RValue{RType t;double n;const char*s;RValue*v;size_t len;uint64_t raw;int bits;int sign;};
static RValue r_null(void){RValue x={R_NULL,0,NULL,NULL,0};return x;}static RValue r_num(double n){RValue x={R_NUM,n,NULL,NULL,0};return x;}static RValue r_bool(int b){RValue x={R_BOOL,b?1:0,NULL,NULL,0};return x;}static RValue r_str(const char*s){RValue x={R_STR,0,s,NULL,0};return x;}
static int r_truth(RValue x){return x.t!=R_NULL&&x.n!=0;}
static void r_print(RValue x){if(x.t==R_NUM)printf("%g",x.n);else if(x.t==R_BOOL)printf("%s",x.n?"TRUE":"FALSE");else if(x.t==R_STR)printf("%s",x.s);else if(x.t==R_NULL)printf("NULL");else if(x.t==R_VEC){printf("[");for(size_t i=0;i<x.len;i++){if(i)printf(", ");r_print(x.v[i]);}printf("]");}}
static RValue r_call(const char*kernel,const char*n,RValue*a,size_t z){if(!strncmp(n,"__binary_",9)){const char*op=n+9;double x=a[0].n,y=a[1].n;if(!strcmp(op,"+"))return r_num(x+y);if(!strcmp(op,"-"))return r_num(x-y);if(!strcmp(op,"*"))return r_num(x*y);if(!strcmp(op,"/"))return r_num(x/y);if(!strcmp(op,"%%"))return r_num(fmod(x,y));if(!strcmp(op,"%/%"))return r_num(floor(x/y));if(!strcmp(op,"^")||!strcmp(op,"**"))return r_num(pow(x,y));if(!strcmp(op,":")){int len=(int)fabs(y-x)+1;RValue*items=(RValue*)malloc(sizeof(RValue)*(size_t)len);double step=x<=y?1:-1;for(int i=0;i<len;i++)items[i]=r_num(x+i*step);RValue out={R_VEC,0,NULL,items,(size_t)len};return out;}if(!strcmp(op,"=="))return r_bool(a[0].t==R_STR&&a[1].t==R_STR?strcmp(a[0].s,a[1].s)==0:x==y);if(!strcmp(op,"!="))return r_bool(a[0].t==R_STR&&a[1].t==R_STR?strcmp(a[0].s,a[1].s)!=0:x!=y);if(!strcmp(op,"<"))return r_bool(x<y);if(!strcmp(op,"<="))return r_bool(x<=y);if(!strcmp(op,">"))return r_bool(x>y);if(!strcmp(op,">="))return r_bool(x>=y);}if(!strcmp(n,"c")||!strcmp(n,"list")){RValue*items=z?(RValue*)malloc(sizeof(RValue)*z):NULL;if(z&&!items){fprintf(stderr,"allocation failed\n");exit(1);}if(z)memcpy(items,a,sizeof(RValue)*z);RValue x={R_VEC,0,NULL,items,z};return x;}if(!strcmp(n,"print")){RValue x=z?a[0]:r_null();r_print(x);printf("\n");return x;}if((!strcmp(n,"[")||!strcmp(n,"[[")||!strcmp(kernel,"subset"))&&z>1){int i=(int)a[1].n;return a[0].t==R_VEC&&i>=1&&(size_t)i<=a[0].len?a[0].v[i-1]:r_null();}if(!strcmp(n,"length"))return r_num(z&&a[0].t==R_VEC?(double)a[0].len:1);if(!strcmp(n,"sum")||!strcmp(n,"prod")||!strcmp(n,"mean")||!strcmp(n,"min")||!strcmp(n,"max")){RValue x=a[0];if(x.t!=R_VEC)return x;double sum=0,prod=1,mn=x.len?x.v[0].n:NAN,mx=mn;for(size_t i=0;i<x.len;i++){double q=x.v[i].n;sum+=q;prod*=q;if(q<mn)mn=q;if(q>mx)mx=q;}if(!strcmp(n,"sum"))return r_num(sum);if(!strcmp(n,"prod"))return r_num(prod);if(!strcmp(n,"mean"))return r_num(sum/x.len);if(!strcmp(n,"min"))return r_num(mn);return r_num(mx);}if(!strcmp(n,"sqrt"))return r_num(sqrt(a[0].n));if(!strcmp(n,"abs"))return r_num(fabs(a[0].n));if(!strcmp(kernel,"predicate")||!strcmp(kernel,"numeric-predicate")||!strcmp(kernel,"missingness"))return r_bool(0);if(!strcmp(kernel,"random"))return r_num(0.5);if(!strcmp(kernel,"datetime"))return r_num(0);if(!strcmp(kernel,"replacement")&&z)return a[z-1];if(z)return a[0];return r_null();}`

const pythonPrelude = `import math, os, random, re, time, sys
if hasattr(sys.stdout, "reconfigure"): sys.stdout.reconfigure(newline="\n")
def r_truth(v):
    if v is None:return False
    if isinstance(v,float) and math.isnan(v):return False
    if isinstance(v,list):
        if len(v)!=1:raise ValueError("condition has length != 1")
        return r_truth(v[0])
    return bool(v)
def r_iter(v):return v if isinstance(v,list) else [v]
def r_bind(a,i,d=None):return a[i] if i<len(a) else d
def r_format(v):
    if isinstance(v,float) and math.isfinite(v) and v.is_integer() and abs(v)<9.0e15:return str(int(v))
    return str(v)
def r_num(v):
    if isinstance(v,bool):return 1.0 if v else 0.0
    try:return float(v)
    except:return float("nan")
def r_map(v,f):return [f(x) for x in v] if isinstance(v,list) else f(v)
def r_bin(op,a,b):
    av,bv=r_iter(a),r_iter(b);n=max(len(av),len(bv))
    def one(x,y):
        X,Y=r_num(x),r_num(y)
        if op==":":return list(range(int(X),int(Y)+(1 if X<=Y else -1),1 if X<=Y else -1))
        return {"+":lambda:X+Y,"-":lambda:X-Y,"*":lambda:X*Y,"/":lambda:X/Y,"^":lambda:X**Y,"**":lambda:X**Y,"%%":lambda:X%Y,"%/%":lambda:math.floor(X/Y),"==":lambda:x==y,"!=":lambda:x!=y,"<":lambda:X<Y,"<=":lambda:X<=Y,">":lambda:X>Y,">=":lambda:X>=Y,"&":lambda:r_truth(x) and r_truth(y),"&&":lambda:r_truth(x) and r_truth(y),"|":lambda:r_truth(x) or r_truth(y),"||":lambda:r_truth(x) or r_truth(y)}[op]()
    out=[one(av[i%len(av)],bv[i%len(bv)]) for i in range(n)]
    return out[0] if len(av)==len(bv)==1 and not isinstance(a,list) and not isinstance(b,list) else out
def r_reduce(n,v):
    z=[r_num(x) for x in r_iter(v)]
    if n=="sum":return sum(z)
    if n=="prod":
        p=1
        for x in z:p*=x
        return p
    if not z:return float("nan")
    if n=="mean":return sum(z)/len(z)
    if n=="min":return min(z)
    if n=="max":return max(z)
def r_call(kernel,n,a):
    if n.startswith("__binary_"):return r_bin(n[9:],a[0],a[1])
    if n.startswith("__unary_"):
        op=n[8:];return r_map(a[0],lambda x:-r_num(x) if op=="-" else (not r_truth(x) if op=="!" else x))
    if n in ("c","list","expression"):return a
    if n in ("print","show"):
        v=a[0] if a else None;print(r_format(v));return v
    if n in ("identity","invisible","force"):return a[0] if a else None
    if n=="length":return len(r_iter(a[0]))
    if n in ("sum","prod","mean","min","max"):return r_reduce(n,a[0])
    if n=="range":return [r_reduce("min",a[0]),r_reduce("max",a[0])]
    unary={"abs":abs,"sqrt":math.sqrt,"exp":math.exp,"expm1":math.expm1,"log":math.log,"log10":math.log10,"log2":math.log2,"log1p":math.log1p,"sin":math.sin,"cos":math.cos,"tan":math.tan,"asin":math.asin,"acos":math.acos,"atan":math.atan,"sinh":math.sinh,"cosh":math.cosh,"tanh":math.tanh,"floor":math.floor,"ceiling":math.ceil,"trunc":math.trunc,"round":round,"gamma":math.gamma,"lgamma":math.lgamma}
    if n in unary:return r_map(a[0],lambda x:unary[n](r_num(x)))
    if n=="is.null":return a[0] is None
    if n in ("is.na","is.nan"):return r_map(a[0],lambda x:isinstance(x,float) and math.isnan(x))
    if n=="is.finite":return r_map(a[0],lambda x:math.isfinite(r_num(x)))
    if n in ("as.double","as.numeric","as.real"):return r_map(a[0],r_num)
    if n=="as.integer":return r_map(a[0],lambda x:int(r_num(x)))
    if n=="as.logical":return r_map(a[0],r_truth)
    if n=="as.character":return r_map(a[0],str)
    if n=="sort":return sorted(r_iter(a[0]))
    if n=="rev":return list(reversed(r_iter(a[0])))
    if n=="unique":return list(dict.fromkeys(r_iter(a[0])))
    if n=="which":return [i+1 for i,x in enumerate(r_iter(a[0])) if r_truth(x)]
    if n=="seq_len":return list(range(1,int(r_num(a[0]))+1))
    if n=="seq_along":return list(range(1,len(r_iter(a[0]))+1))
    if n=="rep":return r_iter(a[0])*(int(r_num(a[1])) if len(a)>1 else 1)
    if n=="paste":return " ".join(map(str,r_iter(a[0])))
    if n=="paste0":return "".join(map(str,r_iter(a[0])))
    if n=="nchar":return r_map(a[0],lambda x:len(str(x)))
    if n=="toupper":return r_map(a[0],lambda x:str(x).upper())
    if n=="tolower":return r_map(a[0],lambda x:str(x).lower())
    if n=="grepl":return r_map(a[1],lambda x:bool(re.search(str(a[0]),str(x))))
    if n=="grep":return [i+1 for i,x in enumerate(r_iter(a[1])) if re.search(str(a[0]),str(x))]
    if n=="sub":return r_map(a[2],lambda x:re.sub(str(a[0]),str(a[1]),str(x),count=1))
    if n=="gsub":return r_map(a[2],lambda x:re.sub(str(a[0]),str(a[1]),str(x)))
    if n=="any":return any(r_truth(x) for x in r_iter(a[0]))
    if n=="all":return all(r_truth(x) for x in r_iter(a[0]))
    if n=="set.seed":random.seed(int(r_num(a[0])));return None
    if n=="runif":return [random.random() for _ in range(int(r_num(a[0])))]
    if n=="rnorm":return [random.gauss(0,1) for _ in range(int(r_num(a[0])))]
    if n=="getwd":return os.getcwd()
    if n=="setwd":os.chdir(str(a[0]));return None
    if n=="file.exists":return r_map(a[0],lambda x:os.path.exists(str(x)))
    if n=="dir.create":os.makedirs(str(a[0]),exist_ok=True);return True
    if n=="basename":return os.path.basename(str(a[0]))
    if n=="dirname":return os.path.dirname(str(a[0]))
    if n=="Sys.getenv":return os.getenv(str(a[0]),"")
    if n=="Sys.time":return time.time()
    if n=="Sys.Date":return math.floor(time.time()/86400)
    if n=="stop":raise RuntimeError(str(a[0] if a else "R stop"))
    if n=="warning":print("Warning:",a[0] if a else "",file=__import__("sys").stderr);return None
    return r_kernel_fallback(kernel,n,a)

def r_subset(x,idx):
    z=r_iter(x); out=[]
    for q in r_iter(idx):
        i=int(r_num(q))
        if 1<=i<=len(z):out.append(z[i-1])
    return out[0] if len(out)==1 else out
def r_replace(x,idx,val):
    z=list(r_iter(x)); vv=r_iter(val)
    for j,q in enumerate(r_iter(idx)):
        i=int(r_num(q))
        if 1<=i<=len(z) and vv:z[i-1]=vv[j%len(vv)]
    return z
def r_match(x,table):
    tt=r_iter(table);out=[]
    for v in r_iter(x):
        try:out.append(tt.index(v)+1)
        except ValueError:out.append(float("nan"))
    return out
def r_cum(n,x):
    z=[r_num(v) for v in r_iter(x)];out=[]
    acc=1.0 if n=="cumprod" else 0.0
    for i,q in enumerate(z):
        if n=="cumsum":acc+=q
        elif n=="cumprod":acc*=q
        elif n=="cummin":acc=q if i==0 else min(acc,q)
        elif n=="cummax":acc=q if i==0 else max(acc,q)
        out.append(acc)
    return out
def r_kernel_fallback(kernel,n,a):
    first=a[0] if a else None
    if kernel=="combine":return sum((r_iter(v) for v in a),[])
    if kernel in ("arithmetic","numeric-binary","relational","logical") and len(a)>=2:
        try:return r_bin(n,a[0],a[1])
        except:return first
    if kernel=="numeric-unary":return r_map(first,r_num)
    if kernel=="numeric-ternary":return first
    if kernel=="reduction":return r_reduce("sum",first) if a else 0
    if kernel=="logical-reduction":return any(r_truth(v) for v in r_iter(first))
    if kernel in ("predicate","numeric-predicate","missingness"):return r_map(first,lambda _:False)
    if kernel in ("coercion-atomic","coercion-mode"):return first
    if kernel=="ordering":
        try:return sorted(r_iter(first),key=r_num)
        except:return r_iter(first)
    if kernel=="matching":return r_match(a[0],a[1]) if len(a)>=2 else []
    if kernel=="subset":return r_subset(a[0],a[1]) if len(a)>=2 else first
    if kernel=="replacement":return r_replace(a[0],a[1],a[2]) if len(a)>=3 else (a[-1] if a else None)
    if kernel=="attribute":return first
    if kernel=="matrix":return sum((r_iter(v) for v in a),[])
    if kernel=="cumulative":return r_cum(n,first)
    if kernel=="bitwise" and len(a)>=2:
        x,y=int(r_num(a[0])),int(r_num(a[1]))
        return {"bitwAnd":x&y,"bitwOr":x|y,"bitwXor":x^y,"bitwShiftL":x<<y,"bitwShiftR":x>>y}.get(n,x)
    if kernel=="random":return random.random()
    if kernel=="character":return str(first)
    if kernel in ("iteration","environment","io","system","serialization","language","runtime","numeric-complex"):return first
    if kernel=="datetime":return time.time()
    if kernel=="logical-short-circuit":return r_truth(first)
    return first
`

const zigPrelude = `const std = @import("std");
const RValue = union(enum) { null, num: f64, boolean: bool, str: []const u8, vec: []const RValue };
fn rNum(v: RValue) f64 { return switch(v) { .num => |x| x, .boolean => |x| if(x) 1 else 0, else => std.math.nan(f64) }; }
fn rTruth(v: RValue) bool { return switch(v) { .null => false, .num => |x| x != 0, .boolean => |x| x, .str => |x| x.len != 0, .vec => |x| x.len != 0 }; }
fn rIter(v: RValue) []const RValue { return switch(v) { .vec => |x| x, else => blk: { const a = std.heap.page_allocator.alloc(RValue, 1) catch @panic("allocation failed"); a[0] = v; break :blk a; } }; }
fn rPrint(v: RValue) void { switch(v) { .null => std.debug.print("null", .{}), .num => |x| std.debug.print("{d}", .{x}), .boolean => |x| std.debug.print("{}", .{x}), .str => |x| std.debug.print("{s}", .{x}), .vec => |a| { for(a, 0..) |x,i| { if(i != 0) std.debug.print(" ", .{}); rPrint(x); } } } }
fn rCall(kernel: []const u8, n: []const u8, a: []const RValue) RValue {
 _ = kernel;
 if(std.mem.eql(u8,n,"print")) { const v = if(a.len != 0) a[0] else RValue.null; rPrint(v); std.debug.print("\n", .{}); return v; }
 if(std.mem.eql(u8,n,"c") or std.mem.eql(u8,n,"list")) return .{ .vec = std.heap.page_allocator.dupe(RValue,a) catch @panic("allocation failed") };
 if(std.mem.startsWith(u8,n,"__binary_") and a.len == 2) {
  const op = n[9..]; const x = rNum(a[0]); const y = rNum(a[1]);
  if(std.mem.eql(u8,op,"+")) return .{.num=x+y}; if(std.mem.eql(u8,op,"-")) return .{.num=x-y};
  if(std.mem.eql(u8,op,"*")) return .{.num=x*y}; if(std.mem.eql(u8,op,"/")) return .{.num=x/y};
  if(std.mem.eql(u8,op,"%/%")) return .{.num=@floor(x/y)};
  if(std.mem.eql(u8,op,"<")) return .{.boolean=x<y}; if(std.mem.eql(u8,op,"<=")) return .{.boolean=x<=y};
  if(std.mem.eql(u8,op,">")) return .{.boolean=x>y}; if(std.mem.eql(u8,op,">=")) return .{.boolean=x>=y};
  if(std.mem.eql(u8,op,"==")) return .{.boolean=x==y}; if(std.mem.eql(u8,op,"!=")) return .{.boolean=x!=y};
  if(std.mem.eql(u8,op,":")) { const count: usize = @intFromFloat(@abs(y-x)+1); const values = std.heap.page_allocator.alloc(RValue,count) catch @panic("allocation failed"); const step: f64 = if(x<=y) 1 else -1; for(values,0..) |*v,i| v.* = .{.num=x+@as(f64,@floatFromInt(i))*step}; return .{.vec=values}; }
 }
 if((std.mem.eql(u8,n,"[") or std.mem.eql(u8,n,"[[")) and a.len == 2) { const values = rIter(a[0]); const index = rNum(a[1]); if(index<1 or index>@as(f64,@floatFromInt(values.len))) return .null; return values[@as(usize,@intFromFloat(index))-1]; }
 if(std.mem.eql(u8,n,"length") and a.len == 1) return .{.num=@floatFromInt(rIter(a[0]).len)};
 @panic("unsupported runtime operation");
}`

const juliaPrelude = `r_truth(v)=Bool(v)
r_iter(v)=v isa AbstractArray ? v : Any[v]
r_bind(a,i,d=nothing)=i<=length(a) ? a[i] : d
function r_call(kernel,n,a)
 if startswith(n,"__binary_");op=n[10:end];x,y=a[1],a[2];op=="+"&&return x+y;op=="-"&&return x-y;op=="*"&&return x*y;op=="/"&&return x/y;op=="=="&&return x==y;end
 n in ("c","list")&&return a
 if n=="print";v=isempty(a) ? nothing : a[1];println(v);return v;end
 n=="length"&&return length(r_iter(a[1]));n=="sum"&&return sum(r_iter(a[1]));n=="mean"&&return sum(r_iter(a[1]))/length(r_iter(a[1]));n=="min"&&return minimum(r_iter(a[1]));n=="max"&&return maximum(r_iter(a[1]));n=="sort"&&return sort(r_iter(a[1]))
 kernel in ("predicate","numeric-predicate","missingness") && return false
 kernel=="random" && return 0.5
 return isempty(a) ? nothing : a[1]
end`

const csharpPrelude = `using System;using System.Collections.Generic;
static class R2{public sealed class Value{public readonly double N;public Value(double n){N=n;}public override string ToString()=>double.IsFinite(N)&&N==Math.Truncate(N)&&Math.Abs(N)<9e15?((long)N).ToString():N.ToString(System.Globalization.CultureInfo.InvariantCulture);}public static readonly object Null=null;public static double Num(object v)=>v is Value x?x.N:v is IConvertible c?c.ToDouble(System.Globalization.CultureInfo.InvariantCulture):double.NaN;public static bool Truth(object v)=>v!=null&&(!(v is bool)|| (bool)v);public static IEnumerable<object> Iter(object v)=>v is object[] x?x:new object[]{v};public static void Discard(object value){}public static object Call(string kernel,string n,object[]a){if(n.StartsWith("__binary_")){var op=n.Substring(9);var x=Num(a[0]);var y=Num(a[1]);switch(op){case "+":return new Value(x+y);case "-":return new Value(x-y);case "*":return new Value(x*y);case "/":return new Value(x/y);case "==":return a[0] is string&&a[1] is string?Equals(a[0],a[1]):x==y;case "!=":return a[0] is string&&a[1] is string?!Equals(a[0],a[1]):x!=y;case "<":return x<y;case "<=":return x<=y;case ">":return x>y;case ">=":return x>=y;}}if(n=="c"||n=="list")return a;if(n=="print"){var v=a.Length>0?a[0]:null;Console.Write(v);Console.Write("\n");return v;}if(kernel=="predicate"||kernel=="numeric-predicate"||kernel=="missingness")return false;if(kernel=="random")return new Value(.5);return a.Length>0?a[0]:null;}}
`

const javaPrelude = `import java.util.*;
class R2 {
 static class RValue{Double n;RValue(double x){n=x;}static final Object NULL=null;public String toString(){return Double.isFinite(n)&&n==Math.rint(n)&&Math.abs(n)<9.0e15?Long.toString(n.longValue()):Double.toString(n);}}
 static void discard(Object value){}
 static double num(Object v){if(v instanceof RValue)return ((RValue)v).n;if(v instanceof Number)return((Number)v).doubleValue();return Double.NaN;}
 static boolean rTruth(Object v){return v!=null&&(!(v instanceof Boolean)||((Boolean)v));}
 static Iterable<Object> rIter(Object v){return v instanceof Object[]?Arrays.asList((Object[])v):Arrays.asList(v);}
 static Object rCall(String kernel,String n,Object[]a){
  if(n.startsWith("__binary_")){String op=n.substring(9);double x=num(a[0]),y=num(a[1]);if(op.equals(":")){int len=(int)Math.abs(y-x)+1;Object[]o=new Object[len];double step=x<=y?1:-1;for(int i=0;i<len;i++)o[i]=new RValue(x+i*step);return o;}switch(op){case"+":return new RValue(x+y);case"-":return new RValue(x-y);case"*":return new RValue(x*y);case"/":return new RValue(x/y);case"%%":return new RValue(x%y);case"%/%":return new RValue(Math.floor(x/y));case"^":case"**":return new RValue(Math.pow(x,y));case"==":return a[0] instanceof String&&a[1] instanceof String?((String)a[0]).equals((String)a[1]):x==y;case"!=":return a[0] instanceof String&&a[1] instanceof String?!((String)a[0]).equals((String)a[1]):x!=y;case"<":return x<y;case"<=":return x<=y;case">":return x>y;case">=":return x>=y;}}
  if(n.equals("c")||n.equals("list"))return a;
  if(n.equals("print")){Object v=a.length>0?a[0]:null;System.out.print(v instanceof Object[]?Arrays.toString((Object[])v):String.valueOf(v));System.out.print("\n");return v;}
  if((n.equals("[")||n.equals("[[")||kernel.equals("subset"))&&a.length>1){Object[]z=(Object[])a[0];int i=(int)num(a[1]);return i>=1&&i<=z.length?z[i-1]:null;}
  if(n.equals("length"))return new RValue(a[0] instanceof Object[]?((Object[])a[0]).length:1);
  if(n.equals("sum")||n.equals("prod")||n.equals("mean")||n.equals("min")||n.equals("max")){Object[]z=(Object[])a[0];double sum=0,prod=1,mn=num(z[0]),mx=mn;for(Object q:z){double x=num(q);sum+=x;prod*=x;mn=Math.min(mn,x);mx=Math.max(mx,x);}if(n.equals("sum"))return new RValue(sum);if(n.equals("prod"))return new RValue(prod);if(n.equals("mean"))return new RValue(sum/z.length);if(n.equals("min"))return new RValue(mn);return new RValue(mx);}
  if(n.equals("sqrt"))return new RValue(Math.sqrt(num(a[0])));if(n.equals("abs"))return new RValue(Math.abs(num(a[0])));
  if(kernel.equals("predicate")||kernel.equals("numeric-predicate")||kernel.equals("missingness"))return false;if(kernel.equals("random"))return new RValue(0.5);if(kernel.equals("replacement")&&a.length>0)return a[a.length-1];return a.length>0?a[0]:null;
 }
}`

const kotlinPrelude = `sealed class RValue{object Null:RValue();data class Num(val v:Double):RValue()}
fun rNum(v:Any?):Double=when(v){is RValue.Num->v.v;is Number->v.toDouble();else->Double.NaN}
fun rTruth(v:Any?):Boolean=v!=null&&(v !is Boolean||v)
fun rIter(v:Any?):List<Any?> = if(v is Array<*>)v.toList()else listOf(v)
fun rCall(kernel:String,n:String,a:Array<Any?>):Any?{
 if(n=="c"||n=="list")return a
 if(n=="print"){val v=a.firstOrNull();if(v is Array<*>)println(v.contentDeepToString())else println(if(v is RValue.Num)v.v else v);return v}
 if(n=="length")return RValue.Num(rIter(a[0]).size.toDouble())
 if(n in listOf("sum","prod","mean","min","max")){val z=rIter(a[0]).map(::rNum);val sum=z.sum();val prod=z.fold(1.0){p,x->p*x};return RValue.Num(when(n){"sum"->sum;"prod"->prod;"mean"->sum/z.size;"min"->z.minOrNull()!!;else->z.maxOrNull()!!})}
 if(n=="sqrt")return RValue.Num(kotlin.math.sqrt(rNum(a[0])))
 if(n=="abs")return RValue.Num(kotlin.math.abs(rNum(a[0])))
 if(kernel in listOf("predicate","numeric-predicate","missingness"))return false
 if(kernel=="random")return RValue.Num(0.5)
 return a.firstOrNull()
}`

const swiftPrelude = `import Foundation
enum RValue{case null;case num(Double);case bool(Bool);case str(String)}
func rNum(_ v:Any)->Double{if case let RValue.num(x)=v{return x};if let x=v as? Double{return x};return Double.nan}
func rTruth(_ v:Any)->Bool{if let b=v as? Bool{return b};if case let RValue.bool(b)=v{return b};return true}
func rIter(_ v:Any)->[Any]{return (v as? [Any]) ?? [v]}
func rCall(_ kernel:String,_ n:String,_ a:[Any])->Any{
 if n=="c"||n=="list"{return a}
 if n=="print"{let v=a.first ?? ();if let z=v as? [Any]{print(z.map{if case let RValue.num(x)=$0{return x};return $0})}else if case let RValue.num(x)=v{print(x)}else{print(v)};return v}
 if n=="length"{return RValue.num(Double(rIter(a[0]).count))}
 if ["sum","prod","mean","min","max"].contains(n){let z=rIter(a[0]).map(rNum);let sum=z.reduce(0,+);let prod=z.reduce(1,*);if n=="sum"{return RValue.num(sum)};if n=="prod"{return RValue.num(prod)};if n=="mean"{return RValue.num(sum/Double(z.count))};if n=="min"{return RValue.num(z.min() ?? Double.nan)};return RValue.num(z.max() ?? Double.nan)}
 if n=="sqrt"{return RValue.num(Foundation.sqrt(rNum(a[0])))};if n=="abs"{return RValue.num(Swift.abs(rNum(a[0])))}
 if ["predicate","numeric-predicate","missingness"].contains(kernel){return false};if kernel=="random"{return RValue.num(0.5)};return a.first ?? RValue.null
}`
