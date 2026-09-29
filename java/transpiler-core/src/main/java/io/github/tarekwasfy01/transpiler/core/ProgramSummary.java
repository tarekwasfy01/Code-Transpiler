package io.github.tarekwasfy01.transpiler.core;
import java.util.*; import java.util.stream.*;
public final class ProgramSummary {
 private ProgramSummary(){}
 public static Map<String,Long> kinds(SemanticProgram p){ return p.nodes().stream().collect(Collectors.groupingBy(SemanticNode::structuralKind,TreeMap::new,Collectors.counting())); }
}
