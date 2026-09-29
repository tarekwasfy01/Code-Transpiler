package io.github.tarekwasfy01.transpiler.core;

import static org.junit.jupiter.api.Assertions.*;
import java.nio.file.*;
import javax.tools.ToolProvider;
import org.junit.jupiter.api.Test;

class JuliaPivotJavaEmitterTest {
 @Test void emitsCompilableControlFlow() throws Exception {
   String pivot="""
     function countStrictPass(rs::Vector{caseResult})::Int
       n = 0
       for r in rs
         if r.StrictPass
           n += 1
         end
       end
       return n
     end
     """;
   String java=JuliaPivotJavaEmitter.emitClass(pivot,"GeneratedSmoke");
   assertTrue(java.contains("public static Object countStrictPass"));
   Path d=Files.createTempDirectory("ct-java-test"), f=d.resolve("GeneratedSmoke.java"); Files.writeString(f,java);
   int rc=ToolProvider.getSystemJavaCompiler().run(null,null,null,"--release","21","-d",d.toString(),f.toString());
   assertEquals(0,rc,java);
 }

 @Test void lowersTupleLetAndOneBasedIndex() throws Exception {
   String pivot="""
     function field(row::Vector{String}, idx::Dict{String, Int}, name::String)::String
       i, ok = let __code_transpiler_key = name; (get(idx, __code_transpiler_key, zero(Int)), haskey(idx, __code_transpiler_key)); end
       if !ok || i >= length(row)
         return ""
       end
       return row[(i) + 1]
     end
     """;
   String java=JuliaPivotJavaEmitter.emitClass(pivot,"GeneratedField");
   assertTrue(java.contains("ctTuple")); assertTrue(java.contains("ctIndex"));
 }
}
