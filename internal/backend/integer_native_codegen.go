package backend

const integerCPrelude = `
static uint64_t r_int_mask(int bits){return bits==64?UINT64_MAX:(UINT64_C(1)<<bits)-1;}
static RValue r_int_value(uint64_t raw,int bits,int sign){RValue v=r_null();v.t=R_INT;v.raw=raw&r_int_mask(bits);v.bits=bits;v.sign=sign;return v;}
static void r_int_error(void){fprintf(stderr,"invalid exact integer operand\n");exit(1);}
static RValue r_exact(const char*name,int bits,int sign,const char*text,RValue*values,size_t count){
 if(!strcmp(name,"integer.literal")){int neg=text[0]=='-';uint64_t raw=strtoull(text+neg,NULL,10);return r_int_value(neg?UINT64_C(0)-raw:raw,bits,sign);}
 for(size_t i=0;i<count;i++){if(values[i].t!=R_INT)r_int_error();if(strcmp(name,"integer.convert")&&(values[i].bits!=bits||values[i].sign!=sign))r_int_error();}
 RValue a=values[0];uint64_t x=a.raw;
 if(!strcmp(name,"integer.value"))return a;
 if(!strcmp(name,"integer.convert")){if(a.sign&&a.bits<64&&(x&(UINT64_C(1)<<(a.bits-1))))x|=~r_int_mask(a.bits);return r_int_value(x,bits,sign);}
 if(!strcmp(name,"integer.format")){char*buffer=(char*)malloc(24);if(!buffer)r_int_error();if(a.sign&&(x&(UINT64_C(1)<<(a.bits-1))))snprintf(buffer,24,"-%" PRIu64,(UINT64_C(0)-x)&r_int_mask(a.bits));else snprintf(buffer,24,"%" PRIu64,x);return r_str(buffer);}
 if(!strcmp(name,"integer.negate"))return r_int_value(UINT64_C(0)-x,bits,sign);
 if(!strcmp(name,"integer.complement"))return r_int_value(~x,bits,sign);
 uint64_t y=values[1].raw;int less=x<y;
 if(sign&&((x^y)&(UINT64_C(1)<<(bits-1))))less=(x&(UINT64_C(1)<<(bits-1)))!=0;
 if(!strcmp(name,"integer.equal"))return r_bool(x==y);
 if(!strcmp(name,"integer.not_equal"))return r_bool(x!=y);
 if(!strcmp(name,"integer.less"))return r_bool(less);
 if(!strcmp(name,"integer.less_equal"))return r_bool(less||x==y);
 if(!strcmp(name,"integer.greater"))return r_bool(!less&&x!=y);
 if(!strcmp(name,"integer.greater_equal"))return r_bool(!less);
 uint64_t result=0;
 if(!strcmp(name,"integer.add"))result=x+y;
 else if(!strcmp(name,"integer.subtract"))result=x-y;
 else if(!strcmp(name,"integer.multiply"))result=x*y;
 else if(!strcmp(name,"integer.and"))result=x&y;
 else if(!strcmp(name,"integer.or"))result=x|y;
 else if(!strcmp(name,"integer.xor"))result=x^y;
 else if(!strcmp(name,"integer.and_not"))result=x&~y;
 else r_int_error();
 return r_int_value(result,bits,sign);
}
`

const integerRustPrelude = `
fn r_int_mask(bits:u32)->u64{if bits==64{u64::MAX}else{(1u64<<bits)-1}}
fn r_int_value(raw:u64,bits:u32,signed:bool)->RValue{RValue::Int(raw&r_int_mask(bits),bits,signed)}
fn r_exact(name:&str,bits:u32,signed:bool,text:&str,values:Vec<RValue>)->RValue{
 if name=="integer.literal"{let neg=text.starts_with('-');let raw=text.trim_start_matches('-').parse::<u64>().unwrap();return r_int_value(if neg{raw.wrapping_neg()}else{raw},bits,signed)}
 let mut operands=Vec::new();for value in values{match value{RValue::Int(raw,b,s)=>{if name!="integer.convert"{assert!(b==bits&&s==signed,"integer operand type mismatch")};operands.push((raw,b,s));},_=>panic!("expected exact integer")}}
 let (mut x,b,s)=operands[0];
 match name{
 "integer.value"=>return r_int_value(x,b,s),
 "integer.convert"=>{if s&&b<64&&(x&(1u64<<(b-1)))!=0{x|=!r_int_mask(b)};return r_int_value(x,bits,signed)},
 "integer.format"=>return RValue::Str(if s&&(x&(1u64<<(b-1)))!=0{format!("-{}",x.wrapping_neg()&r_int_mask(b))}else{x.to_string()}),
 "integer.negate"=>return r_int_value(x.wrapping_neg(),bits,signed),
 "integer.complement"=>return r_int_value(!x,bits,signed),_=>{}}
 let y=operands[1].0;let less=if signed&&((x^y)&(1u64<<(bits-1)))!=0{(x&(1u64<<(bits-1)))!=0}else{x<y};
 let result=match name{
 "integer.equal"=>return RValue::Bool(x==y),"integer.not_equal"=>return RValue::Bool(x!=y),
 "integer.less"=>return RValue::Bool(less),"integer.less_equal"=>return RValue::Bool(less||x==y),
 "integer.greater"=>return RValue::Bool(!less&&x!=y),"integer.greater_equal"=>return RValue::Bool(!less),
 "integer.add"=>x.wrapping_add(y),"integer.subtract"=>x.wrapping_sub(y),"integer.multiply"=>x.wrapping_mul(y),
 "integer.and"=>x&y,"integer.or"=>x|y,"integer.xor"=>x^y,"integer.and_not"=>x&!y,_=>panic!("unsupported integer operation")};
 r_int_value(result,bits,signed)
}
`
