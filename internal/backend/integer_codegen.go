package backend

import (
	_ "embed"
	"strings"
)

// The standalone Go output includes our exact value implementation. There is
// no dependency on the transpiler module or on the original source program.
//
//go:embed integer_value.go
var integerValueSource string

func integerGoPrelude() string {
	_, body, ok := strings.Cut(integerValueSource, "// runtime:start")
	if !ok {
		panic("missing integer runtime boundary")
	}
	return "\n" + body + `func rExact(name string, bits int, signed bool, text string, values []any) any {
 value, err := exactIntegerOperation(name, bits, signed, text, values)
 if err != nil { panic(err) }; return value
}
`
}

const integerPythonPrelude = `
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
    if name == "integer.equal": return a == b
    if name == "integer.not_equal": return a != b
    if name == "integer.less": return a < b
    if name == "integer.less_equal": return a <= b
    if name == "integer.greater": return a > b
    if name == "integer.greater_equal": return a >= b
    if name == "integer.add": result = a + b
    elif name == "integer.subtract": result = a - b
    elif name == "integer.multiply": result = a * b
    elif name == "integer.and": result = a & b
    elif name == "integer.or": result = a | b
    elif name == "integer.xor": result = a ^ b
    elif name == "integer.and_not": result = a & ~b
    else: raise ValueError("unsupported integer operation: " + name)
    return _RExact(result, bits, signed)
`

// BigInteger keeps every fixed-width operation exact. The canonical raw form
// is reduced modulo 2^bits, while value() supplies the signed interpretation.
const integerJavaPrelude = `
class RExact {
 final java.math.BigInteger raw; final int bits; final boolean signed;
 RExact(java.math.BigInteger value,int bits,boolean signed){this.bits=bits;this.signed=signed;this.raw=value.mod(java.math.BigInteger.ONE.shiftLeft(bits));}
 java.math.BigInteger value(){return signed&&raw.testBit(bits-1)?raw.subtract(java.math.BigInteger.ONE.shiftLeft(bits)):raw;}
 public String toString(){return value().toString();}
 static RExact exact(Object value){if(!(value instanceof RExact))throw new IllegalArgumentException("expected exact integer");return(RExact)value;}
 static Object apply(String name,int bits,boolean signed,String text,Object[] args){
  if(name.equals("integer.literal"))return new RExact(new java.math.BigInteger(text),bits,signed);
  RExact a=exact(args[0]);if(!name.equals("integer.convert")&&(a.bits!=bits||a.signed!=signed))throw new IllegalArgumentException("integer type mismatch");
  if(name.equals("integer.value"))return a;if(name.equals("integer.convert"))return new RExact(a.value(),bits,signed);if(name.equals("integer.format"))return a.toString();
  if(name.equals("integer.negate"))return new RExact(a.raw.negate(),bits,signed);if(name.equals("integer.complement"))return new RExact(a.raw.not(),bits,signed);
  RExact b=exact(args[1]);if(b.bits!=bits||b.signed!=signed)throw new IllegalArgumentException("integer type mismatch");java.math.BigInteger x=a.value(),y=b.value();
  if(name.equals("integer.equal"))return a.raw.equals(b.raw);if(name.equals("integer.not_equal"))return !a.raw.equals(b.raw);
  if(name.equals("integer.less"))return x.compareTo(y)<0;if(name.equals("integer.less_equal"))return x.compareTo(y)<=0;if(name.equals("integer.greater"))return x.compareTo(y)>0;if(name.equals("integer.greater_equal"))return x.compareTo(y)>=0;
  if(name.equals("integer.add"))return new RExact(a.raw.add(b.raw),bits,signed);if(name.equals("integer.subtract"))return new RExact(a.raw.subtract(b.raw),bits,signed);if(name.equals("integer.multiply"))return new RExact(a.raw.multiply(b.raw),bits,signed);
  if(name.equals("integer.and"))return new RExact(a.raw.and(b.raw),bits,signed);if(name.equals("integer.or"))return new RExact(a.raw.or(b.raw),bits,signed);if(name.equals("integer.xor"))return new RExact(a.raw.xor(b.raw),bits,signed);if(name.equals("integer.and_not"))return new RExact(a.raw.and(b.raw.not()),bits,signed);
  throw new IllegalArgumentException("unknown exact integer operation: "+name);
 }
}
`

const integerCSharpPrelude = `
sealed class RExact {
 readonly System.Numerics.BigInteger raw; readonly int bits; readonly bool signed;
 RExact(System.Numerics.BigInteger value,int bits,bool signed){this.bits=bits;this.signed=signed;raw=value% (System.Numerics.BigInteger.One<<bits);if(raw.Sign<0)raw+=System.Numerics.BigInteger.One<<bits;}
 System.Numerics.BigInteger Value(){return signed&&raw>= (System.Numerics.BigInteger.One<<(bits-1))?raw-(System.Numerics.BigInteger.One<<bits):raw;}
 public override string ToString()=>Value().ToString();
 static RExact Exact(object value)=>value as RExact??throw new System.ArgumentException("expected exact integer");
 public static object Apply(string name,int bits,bool signed,string text,object[] args){
  if(name=="integer.literal")return new RExact(System.Numerics.BigInteger.Parse(text),bits,signed);var a=Exact(args[0]);if(name!="integer.convert"&&(a.bits!=bits||a.signed!=signed))throw new System.ArgumentException("integer type mismatch");
  if(name=="integer.value")return a;if(name=="integer.convert")return new RExact(a.Value(),bits,signed);if(name=="integer.format")return a.ToString();if(name=="integer.negate")return new RExact(-a.raw,bits,signed);if(name=="integer.complement")return new RExact(~a.raw,bits,signed);
  var b=Exact(args[1]);if(b.bits!=bits||b.signed!=signed)throw new System.ArgumentException("integer type mismatch");var x=a.Value();var y=b.Value();
  if(name=="integer.equal")return a.raw==b.raw;if(name=="integer.not_equal")return a.raw!=b.raw;if(name=="integer.less")return x<y;if(name=="integer.less_equal")return x<=y;if(name=="integer.greater")return x>y;if(name=="integer.greater_equal")return x>=y;
  if(name=="integer.add")return new RExact(a.raw+b.raw,bits,signed);if(name=="integer.subtract")return new RExact(a.raw-b.raw,bits,signed);if(name=="integer.multiply")return new RExact(a.raw*b.raw,bits,signed);if(name=="integer.and")return new RExact(a.raw&b.raw,bits,signed);if(name=="integer.or")return new RExact(a.raw|b.raw,bits,signed);if(name=="integer.xor")return new RExact(a.raw^b.raw,bits,signed);if(name=="integer.and_not")return new RExact(a.raw&~b.raw,bits,signed);throw new System.ArgumentException("unknown exact integer operation: "+name);
 }
}
`
