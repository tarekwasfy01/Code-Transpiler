package io.github.tarekwasfy01.transpiler.core;
import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
/** Auto-detection facade for the semantic formats used by the bootstrap. */
public final class SemanticExchange {
    public enum Format { SE1, SEMANTIC_DOCUMENT_V1 }
    public record Parsed(Format format, SemanticProgram semanticProgram, Se1Document se1) {}
    private SemanticExchange() {}
    public static Parsed parse(Path path) throws IOException {
        String text=Files.readString(path);
        if (text.startsWith("SE/1")) return new Parsed(Format.SE1,null,Se1Parser.parse(text));
        return new Parsed(Format.SEMANTIC_DOCUMENT_V1,SeParser.parse(path),null);
    }
}
