package io.github.tarekwasfy01.transpiler.core;
import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
/** Dependency-free parser for the readable SE/1 exchange envelope. */
public final class Se1Parser {
    private static final String SEM = "--- semantics-json ---";
    private static final String PIVOT = "--- julia-pivot ---";
    private static final String END = "--- end ---";
    private Se1Parser() {}
    public static Se1Document parse(Path path) throws IOException { return parse(Files.readString(path)); }
    public static Se1Document parse(String text) {
        if (text == null || !text.startsWith("SE/1")) throw new IllegalArgumentException("not an SE/1 document");
        String source = value(text, "source:");
        String canonical = value(text, "canonical:");
        int si=text.indexOf(SEM), pi=text.indexOf(PIVOT), ei=text.indexOf(END);
        String json = section(text, si < 0 ? -1 : si+SEM.length(), pi);
        String pivot = section(text, pi < 0 ? -1 : pi+PIVOT.length(), ei);
        return new Se1Document(source, canonical, json, pivot);
    }
    private static String value(String text, String prefix) {
        for (String line : text.split("\R")) if (line.startsWith(prefix)) return line.substring(prefix.length()).trim();
        return "";
    }
    private static String section(String text, int start, int end) {
        if (start < 0 || end < start) return "";
        return text.substring(start,end).strip();
    }
}
