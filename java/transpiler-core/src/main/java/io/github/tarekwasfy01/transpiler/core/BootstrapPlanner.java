package io.github.tarekwasfy01.transpiler.core;
import java.util.*;
public final class BootstrapPlanner {
  private BootstrapPlanner() {}
  public static List<BootstrapRoute> routes(String source, String target) {
    LinkedHashSet<List<String>> r=new LinkedHashSet<>();
    r.add(List.of(source,target));
    for(String pivot:List.of("se","go","rust","julia")) if(!pivot.equals(source)&&!pivot.equals(target)) r.add(List.of(source,pivot,target));
    for(String a:List.of("go","rust","julia","se")) for(String b:List.of("go","rust","julia","se")) if(!a.equals(b)&&!a.equals(source)&&!b.equals(target)) r.add(List.of(source,a,b,target));
    return r.stream().map(BootstrapRoute::new).toList();
  }
}
