package io.github.tarekwasfy01.transpiler.core;
import java.util.List;
public record SemanticProgram(String projection, String evaluation, int indexBase, List<SemanticNode> nodes) {
    public SemanticProgram { nodes = List.copyOf(nodes); }
}
