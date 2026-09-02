package backend

const integerCppPrelude = `
static uint64_t r_int_mask(int bits){return bits==64?UINT64_MAX:(UINT64_C(1)<<bits)-1;}
static RValue r_int_value(uint64_t raw,int bits,bool sign){return RValue(RInteger{raw&r_int_mask(bits),bits,sign});}
static RValue r_exact(const std::string&name,int bits,bool sign,const std::string&text,std::initializer_list<RValue>args){
 if(name=="integer.literal"){bool neg=text[0]=='-';uint64_t raw=std::stoull(neg?text.substr(1):text);return r_int_value(neg?UINT64_C(0)-raw:raw,bits,sign);}
 std::vector<RInteger> values;for(const auto&value:args){auto p=std::get_if<RInteger>(&value.v);if(!p)throw std::runtime_error("expected exact integer");if(name!="integer.convert"&&(p->bits!=bits||p->sign!=sign))throw std::runtime_error("integer type mismatch");values.push_back(*p);}
 auto a=values[0];uint64_t x=a.raw;
 if(name=="integer.value")return r_int_value(x,a.bits,a.sign);
 if(name=="integer.convert"){if(a.sign&&a.bits<64&&(x&(UINT64_C(1)<<(a.bits-1))))x|=~r_int_mask(a.bits);return r_int_value(x,bits,sign);}
 if(name=="integer.format"){if(a.sign&&(x&(UINT64_C(1)<<(a.bits-1))))return RValue("-"+std::to_string((UINT64_C(0)-x)&r_int_mask(a.bits)));return RValue(std::to_string(x));}
 if(name=="integer.negate")return r_int_value(UINT64_C(0)-x,bits,sign);
 if(name=="integer.complement")return r_int_value(~x,bits,sign);
 uint64_t y=values[1].raw;bool less=x<y;if(sign&&((x^y)&(UINT64_C(1)<<(bits-1))))less=(x&(UINT64_C(1)<<(bits-1)))!=0;
 if(name=="integer.equal")return RValue(x==y);if(name=="integer.not_equal")return RValue(x!=y);
 if(name=="integer.less")return RValue(less);if(name=="integer.less_equal")return RValue(less||x==y);
 if(name=="integer.greater")return RValue(!less&&x!=y);if(name=="integer.greater_equal")return RValue(!less);
 uint64_t result=0;
 if(name=="integer.add")result=x+y;else if(name=="integer.subtract")result=x-y;else if(name=="integer.multiply")result=x*y;
 else if(name=="integer.and")result=x&y;else if(name=="integer.or")result=x|y;else if(name=="integer.xor")result=x^y;else if(name=="integer.and_not")result=x&~y;else throw std::runtime_error("unknown integer operation");
 return r_int_value(result,bits,sign);
}
`
