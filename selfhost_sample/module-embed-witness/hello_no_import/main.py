import math, os, random, re, time, sys
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

class _RExact:
    def __init__(self, value, bits, signed):
        self.bits, self.signed = bits, signed
        raw = value % (1 << bits)
        self.value = raw - (1 << bits) if signed and raw >= (1 << (bits - 1)) else raw
    def __str__(self):
        return str(self.value)

def r_exact(name, bits, signed, text, values):
    if name == "integer.literal":
        return _RExact(int(text), bits, signed)
    for value in values:
        if not isinstance(value, _RExact):
            raise TypeError("expected exact integer")
        if name != "integer.convert" and (value.bits != bits or value.signed != signed):
            raise TypeError("integer operand type mismatch")
    a = values[0].value
    if name == "integer.value": return values[0]
    if name == "integer.convert": return _RExact(a, bits, signed)
    if name == "integer.format": return str(a)
    if name == "integer.negate": return _RExact(-a, bits, signed)
    if name == "integer.complement": return _RExact(~a, bits, signed)
    b = values[1].value
    if name == "integer.shift_left": return _RExact(0 if b >= bits else a << b, bits, signed)
    if name == "integer.shift_right": return _RExact((-1 if signed and a < 0 else 0) if b >= bits else (a >> b), bits, signed)
    if name == "integer.equal": return a == b
    if name == "integer.not_equal": return a != b
    if name == "integer.less": return a < b
    if name == "integer.less_equal": return a <= b
    if name == "integer.greater": return a > b
    if name == "integer.greater_equal": return a >= b
    if name == "integer.add": result = a + b
    elif name == "integer.subtract": result = a - b
    elif name == "integer.multiply": result = a * b
    elif name == "integer.divide":
        if b == 0: raise ZeroDivisionError("integer.divide by zero")
        if signed:
            result = (abs(a) // abs(b)) * (-1 if (a < 0) != (b < 0) else 1)
        else:
            result = a // b
    elif name == "integer.remainder":
        if b == 0: raise ZeroDivisionError("integer.remainder by zero")
        if signed:
            result = (abs(a) % abs(b)) * (-1 if a < 0 else 1)
        else:
            result = a % b
    elif name == "integer.and": result = a & b
    elif name == "integer.or": result = a | b
    elif name == "integer.xor": result = a ^ b
    elif name == "integer.and_not": result = a & ~b
    else: raise ValueError("unsupported integer operation: " + name)
    return _RExact(result, bits, signed)


def native_function_0(*__args):
    r_call("runtime", "print", [r_exact("integer.literal", 64, True, "42", [])])
    return None
    return None
