package io.github.tarekwasfy01.transpiler.core;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;

public final class Se1Parser {
    private static final String SEM = "--- semantics-json ---";
    private static final String PIVOT = "--- julia-pivot ---";
    private static final String END = "--- end ---";

    private Se1Parser() {}

    public static Se1Document parse(Path path) throws IOException {
        return parse(Files.readString(path));
    }

    public static Se1Document parse(String text) {
        if (text == null || !text.startsWith("SE/1")) {
            throw new IllegalArgumentException("not an SE/1 document");
        }
        String source = "";
        String canonical = "";
        for (String line : text.lines().toList()) {
            if (line.startsWith("source:")) {
                source = line.substring("source:".length()).trim();
            } else if (line.startsWith("canonical:")) {
                canonical = line.substring("canonical:".length()).trim();
            }
        }
        int si = text.indexOf(SEM);
        int pi = text.indexOf(PIVOT);
        int ei = text.indexOf(END);
        String json = section(text, si < 0 ? -1 : si + SEM.length(), pi);
        String pivot = section(text, pi < 0 ? -1 : pi + PIVOT.length(), ei);
        return new Se1Document(source, canonical, json, pivot);
    }

    private static String section(String text, int start, int end) {
        if (start < 0 || end < start) {
            return "";
        }
        return text.substring(start, end).strip();
    }
}
