package io.github.tarekwasfy01.transpiler.core;

import java.util.*;
import java.util.regex.*;

/** Dependency-free canonical-Julia-pivot -> native Java projector. */
public final class JuliaPivotJavaEmitter {
  private static final ThreadLocal<Set<String>> CURRENT_DECLARED=ThreadLocal.withInitial(HashSet::new);
  private static final Pattern FUNCTION=Pattern.compile("^function\\s+([A-Za-z_$][\\w$]*)\\((.*)\\)(?:::(\\S+))?$");
  private static final Pattern ASSIGN=Pattern.compile("^([A-Za-z_$][\\w$]*(?:\\s*,\\s*[A-Za-z_$][\\w$]*)*)\\s*=\\s*(.+)$");
  private enum Block { FUNCTION, IF, WHILE, FOR }
  private JuliaPivotJavaEmitter(){}

  public static String emitClass(String source,String className){
    className=sanitizeClassName(className);
    List<String> body=new ArrayList<>(); Deque<Block> blocks=new ArrayDeque<>(); Set<String> declared=new HashSet<>(); CURRENT_DECLARED.set(declared);
    int indent=1,temp=0,fnId=0; String fnLabel=null;
    for(String raw:source.split("\\R")){
      String line=raw.trim(); if(line.isEmpty()||line.startsWith("#"))continue; Matcher m;
      if((m=FUNCTION.matcher(line)).matches()){
        declared.clear(); fnLabel="__fn"+(fnId++);
        StringBuilder sig=new StringBuilder("public static Object ").append(m.group(1)).append('(');
        List<String> ps=splitTopLevel(m.group(2),',');
        for(int i=0;i<ps.size();i++){if(i>0)sig.append(", ");Param p=parseParam(ps.get(i));declared.add(p.name());sig.append("Object ").append(p.name());}
        sig.append(") {"); emit(body,indent,sig.toString()); indent++;
        emit(body,indent,"Object __result = null;");
        if(source.contains("__code_transpiler_switch_value")){emit(body,indent,"Object __code_transpiler_switch_value = null;");declared.add("__code_transpiler_switch_value");}
        emit(body,indent,fnLabel+": {"); indent++; blocks.push(Block.FUNCTION); continue;
      }
      if(blocks.isEmpty())continue;
      if(line.equals("end")){
        Block b=blocks.pop(); indent--; emit(body,indent,"}");
        if(b==Block.FUNCTION){emit(body,indent,"return __result;");indent--;emit(body,indent,"}");fnLabel=null;}
        continue;
      }
      if(line.startsWith("elseif ")){indent--;emit(body,indent,"} else if (ctBool("+expr(line.substring(7))+")) {");indent++;continue;}
      if(line.equals("else")){indent--;emit(body,indent,"} else {");indent++;continue;}
      if(line.startsWith("if ")){emit(body,indent,"if (ctBool("+expr(line.substring(3))+")) {");indent++;blocks.push(Block.IF);continue;}
      if(line.startsWith("while ")){emit(body,indent,"while (ctBool("+expr(line.substring(6))+")) {");indent++;blocks.push(Block.WHILE);continue;}
      if((m=Pattern.compile("^for\\s+(.+?)\\s+in\\s+(.+)$").matcher(line)).matches()){
        String v=m.group(1).trim();String iter=expr(m.group(2));
        if(v.startsWith("(")&&v.endsWith(")"))v=v.substring(1,v.length()-1).trim();
        if(v.contains(",")){String t="__it"+(temp++);emit(body,indent,"for (Object "+t+" : ctIterable("+iter+")) {");indent++;String[] vs=v.split(",");for(int i=0;i<vs.length;i++){String n=vs[i].trim();declared.add(n);emit(body,indent,"Object "+n+" = ctTuple("+t+")["+i+"]; ");}}
        else{if(v.equals("_"))v="__ignored"+(temp++);declared.add(v);emit(body,indent,"for (Object "+v+" : ctIterable("+iter+")) {");indent++;}
        blocks.push(Block.FOR);continue;
      }
      if(line.equals("break")||line.equals("continue")){emit(body,indent,line+";");continue;}
      if(line.equals("return")){emit(body,indent,"__result = null; break "+fnLabel+";");continue;}
      if((m=Pattern.compile("^return\\s+(.+)$").matcher(line)).matches()){emit(body,indent,"__result = "+expr(m.group(1))+"; break "+fnLabel+";");continue;}
      if((m=Pattern.compile("^panic\\((.*)\\)$").matcher(line)).matches()){emit(body,indent,"throw new IllegalStateException(String.valueOf("+expr(m.group(1))+"));");continue;}
      String untyped=stripType(line);
      if((m=Pattern.compile("^([A-Za-z_$][\\w$]*)\\s*([+\\-*/%])=\\s*(.+)$").matcher(untyped)).matches()){
        String n=m.group(1),op=m.group(2),rhs=expr(m.group(3));
        String call=switch(op){case "+"->"ctAdd";case "-"->"ctSub";case "*"->"ctMul";case "/"->"ctDiv";default->"ctMod";};
        if(!declared.contains(n))declared.add(n); emit(body,indent,n+" = "+call+"("+n+","+rhs+");"); continue;
      }
      int assignAt=findTopLevelAssignment(untyped);
      if(assignAt>0){String lhsAll=untyped.substring(0,assignAt).trim(),rhsAll=untyped.substring(assignAt+1).trim();List<String> lhsParts=splitTopLevel(lhsAll,',');
        if(lhsParts.size()>1&&lhsParts.stream().allMatch(x->x.matches("[A-Za-z_$][\\w$]*(?:\\.[A-Za-z_$][\\w$]*)+"))&&rhsAll.startsWith("(")&&rhsAll.endsWith(")")){
          List<String> rhsParts=splitTopLevel(rhsAll.substring(1,rhsAll.length()-1),',');String t="__fields"+(temp++);emit(body,indent,"Object[] "+t+" = new Object[]{"+rhsParts.stream().map(JuliaPivotJavaEmitter::expr).reduce((a,b)->a+", "+b).orElse("")+"};");
          for(int i=0;i<lhsParts.size()&&i<rhsParts.size();i++){String lhs=lhsParts.get(i).trim();int dot=lhs.lastIndexOf('.');emit(body,indent,"ctSetField("+expr(lhs.substring(0,dot))+",\""+lhs.substring(dot+1)+"\","+t+"["+i+"]);");}
          continue;
        }
      }
      if((m=Pattern.compile("^([A-Za-z_$][\\w$]*(?:\\.[A-Za-z_$][\\w$]*)+)\\s*=\\s*(.+)$").matcher(untyped)).matches()){
        String lhs=m.group(1),rhs=expr(m.group(2));int dot=lhs.lastIndexOf('.');emit(body,indent,"ctSetField("+expr(lhs.substring(0,dot))+",\""+lhs.substring(dot+1)+"\","+rhs+");");continue;
      }
      if((m=Pattern.compile("^(.+)\\[(.+)]\\s*=\\s*(.+)$").matcher(untyped)).matches()){
        emit(body,indent,"ctSetIndex("+expr(m.group(1))+","+expr(m.group(2))+","+expr(m.group(3))+");");continue;
      }
      if((m=ASSIGN.matcher(untyped)).matches()){
        String[] names=Arrays.stream(m.group(1).split(",")).map(String::trim).toArray(String[]::new);String rhs=m.group(2);
        if(names.length>1){String t="__tmp"+(temp++);emit(body,indent,"Object[] "+t+" = ctTuple("+expr(rhs)+");");for(int i=0;i<names.length;i++){String n=names[i];if(n.equals("_"))n="__ignored"+(temp++);emit(body,indent,(declared.add(n)?"Object ":"")+n+" = "+t+"["+i+"]; ");}}
        else{String n=names[0];if(n.equals("_"))n="__ignored"+(temp++);emit(body,indent,(declared.add(n)?"Object ":"")+n+" = "+expr(rhs)+";");}
        continue;
      }
      emit(body,indent,"__result = "+expr(untyped)+";");
    }
    while(!blocks.isEmpty()){
      Block b=blocks.pop();indent--;emit(body,indent,"}");if(b==Block.FUNCTION){emit(body,indent,"return __result;");indent--;emit(body,indent,"}");}
    }
    StringBuilder out=new StringBuilder();
    out.append("// Generated from canonical Julia pivot by Code-Transpiler\n");
    out.append("import java.lang.reflect.*;\nimport java.util.*;\n\n");
    out.append("public final class ").append(className).append(" {\n");
    out.append(helpers(className)); for(String s:body)out.append(s).append('\n'); out.append("}\n"); return out.toString();
  }

  private record Param(String name){}
  private static Param parseParam(String p){p=p.trim().replace("...","");int k=p.indexOf("::");return new Param(javaIdentifier((k>=0?p.substring(0,k):p).trim()));}
  private static String javaIdentifier(String s){
    if(s.equals("_")) return "__ignored";
    Set<String> keywords=Set.of("abstract","assert","boolean","break","byte","case","catch","char","class","const","continue","default","do","double","else","enum","extends","final","finally","float","for","goto","if","implements","import","instanceof","int","interface","long","native","new","package","private","protected","public","return","short","static","strictfp","super","switch","synchronized","this","throw","throws","transient","try","void","volatile","while","record","sealed","permits","var","yield");
    return keywords.contains(s)?"__"+s:s;
  }
  private static String stripType(String s){return s.replaceAll("::[A-Za-z_$][\\w$.]*(?:\\{[^}]*})?","");}
  static String expr(String s){
    s=stripType(s.trim()); if(s.equals("nothing"))return "null"; if(s.equals("true")||s.equals("false")||s.matches("[-+]?\\d+(?:\\.\\d+)?"))return s;
    if((s.startsWith("\"")&&s.endsWith("\""))||(s.startsWith("'")&&s.endsWith("'")))return s.startsWith("'")?"\""+escape(s.substring(1,s.length()-1))+"\"":s;
    if(s.startsWith("(")&&s.endsWith(")")&&balancedOuterParens(s))return expr(s.substring(1,s.length()-1));
    if(s.matches("\\[\\][A-Za-z_$][\\w$]*(?:\\([^)]*\\))?"))return "new ArrayList<>()";
    if(s.equals("[]"))return "new ArrayList<>()";
    if(s.startsWith("[")&&s.endsWith("]")){
      String inner=s.substring(1,s.length()-1).trim();if(inner.isEmpty())return "new ArrayList<>()";
      List<String> xs=splitTopLevel(inner,',');
      if(xs.size()==1&&xs.get(0).endsWith("..."))return "ctList("+expr(xs.get(0).substring(0,xs.get(0).length()-3))+ ")";
      return "new Object[]{"+xs.stream().map(JuliaPivotJavaEmitter::expr).reduce((a,b)->a+", "+b).orElse("")+"}";
    }
    for(String op:List.of("||","&&","==","!=","<=",">=","<",">","+","-","*","/","%")){int i=findTopLevelOperator(s,op);if(i>0){String a=expr(s.substring(0,i)),b=expr(s.substring(i+op.length()));return switch(op){case "||"->"ctBool("+a+") || ctBool("+b+")";case "&&"->"ctBool("+a+") && ctBool("+b+")";case "=="->"ctEq("+a+","+b+")";case "!="->"!ctEq("+a+","+b+")";case "<"->"ctCmp("+a+","+b+") < 0";case ">"->"ctCmp("+a+","+b+") > 0";case "<="->"ctCmp("+a+","+b+") <= 0";case ">="->"ctCmp("+a+","+b+") >= 0";case "+"->"ctAdd("+a+","+b+")";case "-"->"ctSub("+a+","+b+")";case "*"->"ctMul("+a+","+b+")";case "/"->"ctDiv("+a+","+b+")";default->"ctMod("+a+","+b+")";};}}
    if(s.startsWith("!"))return "!ctBool("+expr(s.substring(1))+")";
    if(s.endsWith("]")){int open=findTrailingIndexOpen(s);if(open>0){String base=s.substring(0,open).trim(),ix=s.substring(open+1,s.length()-1).trim();Matcher plus=Pattern.compile("^\\((.+)\\)\\s*\\+\\s*1$").matcher(ix);return "ctIndex("+expr(base)+","+(plus.matches()?expr(plus.group(1)):"ctSub("+expr(ix)+",1)")+")";}}
    Matcher field=Pattern.compile("^([A-Za-z_$][\\w$]*)(\\.[A-Za-z_$][\\w$]*)+$").matcher(s);if(field.matches()){String[] parts=s.split("\\.");String out=expr(parts[0]);for(int i=1;i<parts.length;i++)out="ctField("+out+",\""+parts[i]+"\")";return out;}
    Matcher c=Pattern.compile("^([A-Za-z_$][\\w$.!]*)\\((.*)\\)$").matcher(s);if(c.matches()){
      String name=c.group(1),rawArgs=c.group(2).trim();List<String> as=splitTopLevel(rawArgs,',');String joined=as.stream().map(JuliaPivotJavaEmitter::expr).reduce((a,b)->a+", "+b).orElse("");return switch(name){case "length"->"ctLength("+joined+")";case "string","String"->"String.valueOf("+joined+")";case "uppercase"->"String.valueOf("+joined+").toUpperCase(Locale.ROOT)";case "lowercase"->"String.valueOf("+joined+").toLowerCase(Locale.ROOT)";case "int","uint64","uintptr"->"ctLong("+joined+")";case "zero"->"0L";case "values"->"ctValues("+joined+")";case "haskey"->"ctHasKey("+joined+")";case "get"->"ctGet("+joined+")";case "Dict"->"new LinkedHashMap<>()";default->"ctCall(\""+name.replace("!","")+"\""+(joined.isBlank()?"":", "+joined)+")";};}
    if(s.matches("[A-Za-z_$][\\w$]*")){String id=javaIdentifier(s);return CURRENT_DECLARED.get().contains(s)||CURRENT_DECLARED.get().contains(id)?id:"ctGlobal(\""+escape(s)+"\")";}
    return s;
  }
  private static boolean balancedOuterParens(String s){int d=0;for(int i=0;i<s.length();i++){char c=s.charAt(i);if(c=='(')d++;if(c==')')d--;if(d==0&&i<s.length()-1)return false;}return d==0;}
  private static int findTopLevelOperator(String s,String op){
    int dp=0,db=0,dc=0,last=-1; boolean str=false,esc=false; char q=0;
    for(int i=0;i<=s.length()-op.length();i++){
      char ch=s.charAt(i);
      if(str){if(esc){esc=false;continue;}if(ch=='\\'){esc=true;continue;}if(ch==q)str=false;continue;}
      if(ch=='"'||ch=='\''){str=true;q=ch;continue;}
      if(ch=='(')dp++; else if(ch==')')dp--; else if(ch=='[')db++; else if(ch==']')db--; else if(ch=='{')dc++; else if(ch=='}')dc--;
      if(dp!=0||db!=0||dc!=0||!s.startsWith(op,i))continue;
      if((op.equals("+")||op.equals("-"))&&(i==0||"(,[=:+-*/%!<>|&".indexOf(s.charAt(i-1))>=0))continue;
      if((op.equals("<")||op.equals(">"))&&i+1<s.length()&&s.charAt(i+1)=='=')continue;
      last=i;
    }
    return last;
  }
  private static int findTopLevelAssignment(String s){int d=0;boolean str=false,esc=false;char q=0;for(int i=0;i<s.length();i++){char ch=s.charAt(i);if(str){if(esc){esc=false;continue;}if(ch=='\\'){esc=true;continue;}if(ch==q)str=false;continue;}if(ch=='"'||ch=='\''){str=true;q=ch;continue;}if(ch=='('||ch=='['||ch=='{')d++;else if(ch==')'||ch==']'||ch=='}')d--;else if(ch=='='&&d==0&&(i+1>=s.length()||s.charAt(i+1)!='=')&&(i==0||"!<>=".indexOf(s.charAt(i-1))<0))return i;}return -1;}
  private static int findTrailingIndexOpen(String s){int d=0;for(int i=s.length()-1;i>=0;i--){char ch=s.charAt(i);if(ch==']')d++;else if(ch=='['){d--;if(d==0)return i;}}return -1;}
  private static List<String> splitTopLevel(String s,char sep){List<String>o=new ArrayList<>();int st=0,d=0;boolean str=false;char q=0;for(int i=0;i<s.length();i++){char c=s.charAt(i);if(str){if(c==q&&(i==0||s.charAt(i-1)!='\\'))str=false;continue;}if(c=='"'||c=='\''){str=true;q=c;}else if(c=='('||c=='['||c=='{')d++;else if(c==')'||c==']'||c=='}')d--;else if(c==sep&&d==0){o.add(s.substring(st,i).trim());st=i+1;}}if(st<s.length())o.add(s.substring(st).trim());return o;}
  private static void emit(List<String>o,int i,String s){o.add("  ".repeat(Math.max(0,i))+s);}
  private static String sanitizeClassName(String s){String x=s.replaceAll("[^A-Za-z0-9_$]","_");if(x.isEmpty()||!Character.isJavaIdentifierStart(x.charAt(0)))x="Generated_"+x;return x;}
  private static String escape(String s){return s.replace("\\","\\\\").replace("\"","\\\"");}
  private static String helpers(String cls){return ("""
  private static boolean ctBool(Object v) { if (v instanceof Boolean b) return b; if (v instanceof Number n) return n.doubleValue()!=0; return v!=null; }
  private static long ctLong(Object v) { return v instanceof Number n ? n.longValue() : Long.parseLong(String.valueOf(v)); }
  private static int ctLength(Object v) { if(v==null)return 0; if(v instanceof Collection<?> c)return c.size(); if(v instanceof Map<?,?> m)return m.size(); if(v instanceof CharSequence c)return c.length(); if(v.getClass().isArray())return Array.getLength(v); return 0; }
  private static boolean ctEq(Object a,Object b){return Objects.equals(a,b);}
  private static int ctCmp(Object a,Object b){if(a instanceof Number x&&b instanceof Number y)return Double.compare(x.doubleValue(),y.doubleValue());return String.valueOf(a).compareTo(String.valueOf(b));}
  private static Object ctAdd(Object a,Object b){if(a instanceof Number x&&b instanceof Number y){if(a instanceof Double||b instanceof Double||a instanceof Float||b instanceof Float)return x.doubleValue()+y.doubleValue();return x.longValue()+y.longValue();}return String.valueOf(a)+String.valueOf(b);}
  private static Object ctSub(Object a,Object b){return ((Number)a).doubleValue()-((Number)b).doubleValue();}
  private static Object ctMul(Object a,Object b){return ((Number)a).doubleValue()*((Number)b).doubleValue();}
  private static Object ctDiv(Object a,Object b){return ((Number)a).doubleValue()/((Number)b).doubleValue();}
  private static Object ctMod(Object a,Object b){return ((Number)a).longValue()%((Number)b).longValue();}
  private static Object[] ctTuple(Object v){if(v instanceof Object[] a)return a;if(v instanceof List<?> l)return l.toArray();return new Object[]{v};}
  private static Iterable<?> ctIterable(Object v){if(v instanceof Iterable<?> i)return i;if(v!=null&&v.getClass().isArray()){List<Object>x=new ArrayList<>();for(int i=0;i<Array.getLength(v);i++)x.add(Array.get(v,i));return x;}return List.of();}
  private static Object ctIndex(Object v,Object i){int n=(int)ctLong(i);if(v instanceof List<?> l)return l.get(n);if(v instanceof CharSequence c)return String.valueOf(c.charAt(n));if(v!=null&&v.getClass().isArray())return Array.get(v,n);if(v instanceof Map<?,?> m)return m.get(i);return null;}
  @SuppressWarnings({"rawtypes","unchecked"}) private static Object ctSetIndex(Object v,Object i,Object x){int n=(int)ctLong(i);if(v instanceof List l){while(l.size()<=n)l.add(null);l.set(n,x);return x;}if(v instanceof Map m){m.put(i,x);return x;}if(v!=null&&v.getClass().isArray()){Array.set(v,n,x);return x;}return x;}
  @SuppressWarnings({"rawtypes","unchecked"}) private static Object ctSetField(Object v,String name,Object x){if(v instanceof Map m){m.put(name,x);return x;}if(v!=null)try{Field f=v.getClass().getField(name);f.set(v,x);return x;}catch(ReflectiveOperationException ignored){}return x;}
  private static Object ctField(Object v,String name){if(v==null)return null;if(v instanceof Map<?,?> m)return m.get(name);try{Field f=v.getClass().getField(name);return f.get(v);}catch(ReflectiveOperationException ignored){}return null;}
  private static Object ctGlobal(String name){return name;}
  private static Collection<?> ctValues(Object v){return v instanceof Map<?,?> m?m.values():v instanceof Collection<?> c?c:List.of();}
  private static boolean ctHasKey(Object v,Object k){return v instanceof Map<?,?> m&&m.containsKey(k);}
  private static Object ctGet(Object v,Object k,Object d){if(v instanceof Map<?,?> m)return m.containsKey(k)?m.get(k):d;return d;}
  private static Object ctCall(String name,Object... args){try{for(Method m:__CLASS__.class.getDeclaredMethods()){if(m.getName().equals(name)&&m.getParameterCount()==args.length)return m.invoke(null,args);}}catch(ReflectiveOperationException e){throw new IllegalStateException(e);}throw new UnsupportedOperationException("unresolved pivot call: "+name);}
""").replace("__CLASS__",cls);}
}
