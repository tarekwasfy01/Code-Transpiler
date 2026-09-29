package io.github.tarekwasfy01.transpiler.core;
import java.util.*;
/** Native Java port of the Rust registry/capability core. */
public final class LanguageRegistry {
  public record FrontendSpec(String id,List<String> aliases,List<String> extensions,List<String> capabilities) { public FrontendSpec{aliases=List.copyOf(aliases);extensions=List.copyOf(extensions);capabilities=List.copyOf(capabilities);} }
  public record BackendSpec(String id,List<String> aliases,List<String> capabilities) { public BackendSpec{aliases=List.copyOf(aliases);capabilities=List.copyOf(capabilities);} }
  public enum CapabilityStatus { NATIVE, LOWERING, EMULATED, UNSUPPORTED }
  public record CapabilityResult(String feature,String backend,CapabilityStatus status,String reason) {}
  private static FrontendSpec f(String id,String aliases,String exts,String caps){return new FrontendSpec(id,List.of(aliases.split(" ")),exts.isBlank()?List.of():List.of(exts.split(" ")),List.of(caps.split(" ")));}
  private static BackendSpec b(String id,String aliases,String caps){return new BackendSpec(id,List.of(aliases.split(" ")),List.of(caps.split(" ")));}
  private static final List<FrontendSpec> FRONTENDS=List.of(
    f("r","r",".r .R","core lazy_evaluation named_arguments one_based_index"),
    f("go","go",".go","core eager_evaluation"), f("python","python py",".py","core eager_evaluation named_arguments"),
    f("rust","rust rs",".rs","core eager_evaluation"), f("c","c",".c .h","core eager_evaluation"),
    f("cpp","cpp c++",".cpp .cc .cxx .hpp","core eager_evaluation"), f("zig","zig",".zig","core eager_evaluation"),
    f("julia","julia jl",".jl","core eager_evaluation"), f("se","se semantic semantic-exchange",".se","core semantic_exchange"),
    f("nim","nim",".nim","core eager_evaluation"), f("csharp","csharp c# cs",".cs","core eager_evaluation"),
    f("java","java",".java","core eager_evaluation"), f("kotlin","kotlin kt",".kt .kts","core eager_evaluation"),
    f("swift","swift",".swift","core eager_evaluation") );
  private static final List<BackendSpec> BACKENDS=FRONTENDS.stream().map(x->b(x.id(),String.join(" ",x.aliases()),x.id().equals("se")?"core semantic_exchange":"core")).toList();
  private LanguageRegistry(){}
  public static List<FrontendSpec> frontends(){return FRONTENDS;}
  public static List<BackendSpec> backends(){return BACKENDS;}
  public static String normalizeLanguage(String name){String n=name.trim().toLowerCase(Locale.ROOT);return FRONTENDS.stream().filter(s->s.aliases().contains(n)).map(FrontendSpec::id).findFirst().orElse(n);}
  public static boolean hasFrontend(String n){String x=normalizeLanguage(n);return FRONTENDS.stream().anyMatch(s->s.id().equals(x));}
  public static boolean hasBackend(String n){String x=normalizeLanguage(n);return BACKENDS.stream().anyMatch(s->s.id().equals(x));}
  public static CapabilityResult backendCapability(String feature,String backend){String b=normalizeLanguage(backend);if(!hasBackend(b))return new CapabilityResult(feature,b,CapabilityStatus.UNSUPPORTED,"unknown backend");boolean integer=feature.startsWith("integer.")||feature.startsWith("native.integer.")||feature.startsWith("fixed_width_integer.");if(integer&&Set.of("go","python","c","rust","cpp","java","csharp").contains(b))return new CapabilityResult(feature,b,CapabilityStatus.LOWERING,"fixed-width integer operations with explicit wrap semantics");if(feature.equals("core"))return new CapabilityResult(feature,b,CapabilityStatus.LOWERING,"shared semantic core lowering");return new CapabilityResult(feature,b,CapabilityStatus.UNSUPPORTED,"backend has no declared capability");}
}
