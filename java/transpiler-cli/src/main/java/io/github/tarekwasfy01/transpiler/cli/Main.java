package io.github.tarekwasfy01.transpiler.cli;

import io.github.tarekwasfy01.transpiler.core.*;
import java.nio.file.*;

public final class Main {
 public static void main(String[] args) throws Exception {
   if(args.length==0){ usage(); System.exit(2); }
   switch(args[0]){
     case "inspect-se" -> { if(args.length!=2){usage();System.exit(2);} inspect(Path.of(args[1])); }
     case "routes" -> { if(args.length!=3){usage();System.exit(2);} BootstrapPlanner.routes(args[1],args[2]).forEach(r->System.out.println(String.join(" -> ",r.stages()))); }
     case "transpile-se" -> transpileSe(args);
     default -> { usage(); System.exit(2); }
   }
 }
 private static void transpileSe(String[] args) throws Exception {
   if(args.length < 3 || args.length > 4){usage();System.exit(2);}
   Path input=Path.of(args[1]); Path output=Path.of(args[2]); String className=args.length==4?args[3]:className(output);
   var x=SemanticExchange.parse(input);
   if(x.se1()==null) throw new IllegalArgumentException("transpile-se currently requires SE/1 with a canonical Julia pivot");
   String java=JuliaPivotJavaEmitter.emitClass(x.se1().juliaPivot(),className);
   Files.createDirectories(output.toAbsolutePath().getParent()); Files.writeString(output,java);
 }
 private static String className(Path p){String n=p.getFileName().toString();int dot=n.lastIndexOf('.');if(dot>0)n=n.substring(0,dot);return n.isBlank()?"GeneratedProgram":n;}
 private static void inspect(Path p) throws Exception {
   var x=SemanticExchange.parse(p);
   System.out.println("format="+x.format());
   if(x.se1()!=null){var s=x.se1();System.out.println("source="+s.sourceLanguage());System.out.println("canonical="+s.canonicalLanguage());System.out.println("semanticsBytes="+s.semanticsJson().length());System.out.println("pivotBytes="+s.juliaPivot().length());}
   else {var s=x.semanticProgram();System.out.println("projection="+s.projection());System.out.println("evaluation="+s.evaluation());System.out.println("indexBase="+s.indexBase());System.out.println("nodes="+s.nodes().size());ProgramSummary.kinds(s).forEach((k,v)->System.out.println(k+"="+v));}
 }
 private static void usage(){System.err.println("usage:\n  code-transpiler inspect-se <file.se>\n  code-transpiler routes <source> <target>\n  code-transpiler transpile-se <input.se> <output.java> [ClassName]");}
}
