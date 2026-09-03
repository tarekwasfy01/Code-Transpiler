package backend

// Nim values share one tagged representation across scalar expressions,
// vectors, argument bindings and mutable control-flow state slots.
const nimPrelude = `import std/[math, strutils]
type
  RKind = enum rkNull, rkNum, rkBool, rkStr, rkVec
  RValue = object
    case kind: RKind
    of rkNum: number: float64
    of rkBool: boolean: bool
    of rkStr: text: string
    of rkVec: values: seq[RValue]
    of rkNull: discard

# Rows: null, number, boolean, string, vector.
# Columns: numeric projection, scalar truth projection, iteration projection.
const rValueCapabilities: array[RKind, array[3, bool]] = [
  [false, true, true], [true, true, true], [true, true, true],
  [false, false, true], [false, false, true]]
proc rNull(): RValue = RValue(kind: rkNull)
proc rNum(x: float64): RValue = RValue(kind: rkNum, number: x)
proc rBool(x: bool): RValue = RValue(kind: rkBool, boolean: x)
proc rStr(x: string): RValue = RValue(kind: rkStr, text: x)
proc rVec(x: seq[RValue]): RValue = RValue(kind: rkVec, values: x)
proc rNumber(v: RValue): float64 =
  if not rValueCapabilities[v.kind][0]:
    raise newException(ValueError, "value has no scalar numeric projection")
  case v.kind
  of rkNum: return v.number
  of rkBool: return (if v.boolean: 1.0 else: 0.0)
  else: raise newException(ValueError, "invalid numeric projection")
proc rTruth(v: RValue): bool =
  if not rValueCapabilities[v.kind][1]:
    raise newException(ValueError, "value has no scalar truth projection")
  case v.kind
  of rkNull: return false
  of rkBool: return v.boolean
  of rkNum:
    if classify(v.number) == fcNan: raise newException(ValueError, "missing truth value")
    return v.number != 0.0
  else: raise newException(ValueError, "invalid truth projection")
proc rIter(v: RValue): seq[RValue] =
  if not rValueCapabilities[v.kind][2]: raise newException(ValueError, "value is not iterable")
  case v.kind
  of rkNull: return @[]
  of rkVec: return v.values
  else: return @[v]
proc rFormat(v: RValue): string =
  case v.kind
  of rkNull: return "NULL"
  of rkBool: return $v.boolean
  of rkStr: return v.text
  of rkNum:
    if classify(v.number) == fcNan: return "NaN"
    if v.number == Inf: return "Inf"
    if v.number == NegInf: return "-Inf"
    if abs(v.number) < 9.0e15 and v.number == trunc(v.number): return $int64(v.number)
    return $v.number
  of rkVec:
    var parts: seq[string] = @[]
    for item in v.values: parts.add(rFormat(item))
    return parts.join(" ")
proc rEqual(x, y: RValue): bool =
  if rValueCapabilities[x.kind][0] and rValueCapabilities[y.kind][0]:
    return rNumber(x) == rNumber(y)
  if x.kind != y.kind: return false
  case x.kind
  of rkNull: return true
  of rkStr: return x.text == y.text
  of rkVec:
    if x.values.len != y.values.len: return false
    for i in 0..<x.values.len:
      if not rEqual(x.values[i], y.values[i]): return false
    return true
  else: return false
proc rCall(kernel, name: string, args: seq[RValue]): RValue {.discardable.} =
  if name == "print":
    let value = (if args.len > 0: args[0] else: rNull())
    echo rFormat(value)
    return value
  if name == "list": return rVec(args)
  if name == "c":
    var values: seq[RValue] = @[]
    for item in args: values.add(rIter(item))
    return rVec(values)
  if name == "length" and args.len == 1: return rNum(float64(rIter(args[0]).len))
  if (name == "[" or name == "[[") and args.len == 2:
    let values = rIter(args[0])
    let index = rNumber(args[1])
    if classify(index) in {fcNan, fcInf, fcNegInf} or index != trunc(index) or index < 1 or index > float64(values.len):
      raise newException(ValueError, "unsupported or out-of-bounds index")
    return values[int(index)-1]
  if name.startsWith("__binary_") and args.len == 2:
    let op = name[9..^1]
    if op == "==": return rBool(rEqual(args[0], args[1]))
    if op == "!=": return rBool(not rEqual(args[0], args[1]))
    if op == "&&" or op == "&": return rBool(rTruth(args[0]) and rTruth(args[1]))
    if op == "||" or op == "|": return rBool(rTruth(args[0]) or rTruth(args[1]))
    let x = rNumber(args[0])
    let y = rNumber(args[1])
    case op
    of "+": return rNum(x+y)
    of "-": return rNum(x-y)
    of "*": return rNum(x*y)
    of "/": return rNum(x/y)
    of "%/%": return rNum(floor(x/y))
    of "%%": return rNum(x-floor(x/y)*y)
    of "^", "**": return rNum(pow(x,y))
    of "<": return rBool(x<y)
    of "<=": return rBool(x<=y)
    of ">": return rBool(x>y)
    of ">=": return rBool(x>=y)
    of ":":
      let distance = abs(y-x)
      if classify(distance) in {fcNan, fcInf, fcNegInf} or distance > 1000000:
        raise newException(ValueError, "range exceeds supported allocation bound")
      var values: seq[RValue] = @[]
      let step = (if x <= y: 1.0 else: -1.0)
      for i in 0..int(distance): values.add(rNum(x+float64(i)*step))
      return rVec(values)
    else: discard
  raise newException(ValueError, "unsupported runtime operation: " & name)
`
