package io.github.tarekwasfy01.transpiler.core;
import java.util.List;
public record BootstrapRoute(List<String> stages) { public BootstrapRoute { stages=List.copyOf(stages); } }
