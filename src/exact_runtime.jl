"""Exact fixed-width integer cell used by repaired Julia backend output."""
struct RExact
    raw::BigInt
    bits::Int
    signed::Bool
    function RExact(value::Integer, bits::Integer, signed::Bool)
        bits > 0 || throw(ArgumentError("integer width must be positive"))
        modulus = big(1) << Int(bits)
        new(mod(BigInt(value), modulus), Int(bits), signed)
    end
end

function exact_value(x::RExact)
    if x.signed && x.raw >= (big(1) << (x.bits - 1))
        return x.raw - (big(1) << x.bits)
    end
    return x.raw
end

Base.show(io::IO, x::RExact) = print(io, exact_value(x))
Base.string(x::RExact) = string(exact_value(x))
Base.Int(x::RExact) = Int(exact_value(x))

function _expect_exact(value, bits::Int, signed::Bool; allow_convert=false)
    value isa RExact || throw(ArgumentError("expected exact integer"))
    if !allow_convert && (value.bits != bits || value.signed != signed)
        throw(ArgumentError("integer operand type mismatch"))
    end
    return value
end

"""Port of the exact-integer runtime contract used by generated Julia code."""
function r_exact(name::AbstractString, bits::Integer, signed::Bool, text::AbstractString, values)
    n, b = String(name), Int(bits)
    vals = collect(values)
    if n == "integer.literal"
        return RExact(parse(BigInt, String(text)), b, signed)
    end
    isempty(vals) && throw(ArgumentError("$n requires an operand"))
    a = _expect_exact(vals[1], b, signed; allow_convert=(n == "integer.convert"))
    av = exact_value(a)
    n == "integer.value" && return a
    n == "integer.convert" && return RExact(av, b, signed)
    n == "integer.format" && return string(av)
    n == "integer.negate" && return RExact(-av, b, signed)
    n == "integer.complement" && return RExact(~a.raw, b, signed)

    length(vals) >= 2 || throw(ArgumentError("$n requires two operands"))
    rhs = _expect_exact(vals[2], b, signed)
    bv = exact_value(rhs)
    n == "integer.shift_left" && return RExact(bv >= b ? 0 : av << Int(bv), b, signed)
    n == "integer.shift_right" && return RExact(bv >= b ? (signed && av < 0 ? -1 : 0) : av >> Int(bv), b, signed)
    n == "integer.equal" && return a.raw == rhs.raw
    n == "integer.not_equal" && return a.raw != rhs.raw
    n == "integer.less" && return av < bv
    n == "integer.less_equal" && return av <= bv
    n == "integer.greater" && return av > bv
    n == "integer.greater_equal" && return av >= bv
    n == "integer.add" && return RExact(a.raw + rhs.raw, b, signed)
    n == "integer.subtract" && return RExact(a.raw - rhs.raw, b, signed)
    n == "integer.multiply" && return RExact(a.raw * rhs.raw, b, signed)
    n == "integer.divide" && begin
        bv == 0 && throw(DivideError())
        return RExact(div(av, bv), b, signed)
    end
    n == "integer.remainder" && begin
        bv == 0 && throw(DivideError())
        return RExact(rem(av, bv), b, signed)
    end
    n == "integer.and" && return RExact(a.raw & rhs.raw, b, signed)
    n == "integer.or" && return RExact(a.raw | rhs.raw, b, signed)
    n == "integer.xor" && return RExact(xor(a.raw, rhs.raw), b, signed)
    n == "integer.and_not" && return RExact(a.raw & ~rhs.raw, b, signed)
    throw(ArgumentError("unsupported integer operation: $n"))
end

# Standalone source injected into Julia emitted by the legacy Go backend when
# it references r_exact but omitted the helper itself.
const _JULIA_EXACT_RUNTIME_SOURCE = raw"""
struct RExact
    raw::BigInt
    bits::Int
    signed::Bool
    function RExact(value::Integer,bits::Integer,signed::Bool)
        m=big(1)<<Int(bits); new(mod(BigInt(value),m),Int(bits),signed)
    end
end
function exact_value(x::RExact)
    x.signed && x.raw >= (big(1)<<(x.bits-1)) ? x.raw-(big(1)<<x.bits) : x.raw
end
Base.show(io::IO,x::RExact)=print(io,exact_value(x))
function r_exact(name,bits,signed,text,values)
    b=Int(bits); vals=collect(values)
    name=="integer.literal" && return RExact(parse(BigInt,String(text)),b,signed)
    isempty(vals) && error("exact integer operand missing")
    a=vals[1]; a isa RExact || error("expected exact integer")
    if name!="integer.convert" && (a.bits!=b || a.signed!=signed); error("integer operand type mismatch"); end
    av=exact_value(a)
    name=="integer.value" && return a
    name=="integer.convert" && return RExact(av,b,signed)
    name=="integer.format" && return string(av)
    name=="integer.negate" && return RExact(-av,b,signed)
    name=="integer.complement" && return RExact(~a.raw,b,signed)
    length(vals)>=2 || error("second exact integer operand missing")
    z=vals[2]; z isa RExact || error("expected exact integer")
    (z.bits==b && z.signed==signed) || error("integer operand type mismatch")
    bv=exact_value(z)
    name=="integer.shift_left" && return RExact(bv>=b ? 0 : av<<Int(bv),b,signed)
    name=="integer.shift_right" && return RExact(bv>=b ? (signed&&av<0 ? -1 : 0) : av>>Int(bv),b,signed)
    name=="integer.equal" && return a.raw==z.raw
    name=="integer.not_equal" && return a.raw!=z.raw
    name=="integer.less" && return av<bv
    name=="integer.less_equal" && return av<=bv
    name=="integer.greater" && return av>bv
    name=="integer.greater_equal" && return av>=bv
    name=="integer.add" && return RExact(a.raw+z.raw,b,signed)
    name=="integer.subtract" && return RExact(a.raw-z.raw,b,signed)
    name=="integer.multiply" && return RExact(a.raw*z.raw,b,signed)
    name=="integer.divide" && (bv==0 ? throw(DivideError()) : return RExact(div(av,bv),b,signed))
    name=="integer.remainder" && (bv==0 ? throw(DivideError()) : return RExact(rem(av,bv),b,signed))
    name=="integer.and" && return RExact(a.raw&z.raw,b,signed)
    name=="integer.or" && return RExact(a.raw|z.raw,b,signed)
    name=="integer.xor" && return RExact(xor(a.raw,z.raw),b,signed)
    name=="integer.and_not" && return RExact(a.raw&~z.raw,b,signed)
    error("unsupported integer operation: "*String(name))
end
"""

function _repair_julia_output(code::AbstractString)
    text = String(code)
    if occursin("r_exact(", text) && !occursin("function r_exact", text)
        return _JULIA_EXACT_RUNTIME_SOURCE * "\n" * text
    end
    return text
end
