package io.github.tarekwasfy01.transpiler.core;
import java.util.Map;
public record SemanticNode(long id, String structuralKind, Map<String,String> fields) {
    public SemanticNode { fields = Map.copyOf(fields); }
    public String field(String name) { return fields.get(name); }
}
