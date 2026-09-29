package io.github.tarekwasfy01.transpiler.core;

import java.io.BufferedReader;
import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

public final class SeParser {
    private SeParser() {}

    public static SemanticProgram parse(Path path) throws IOException {
        String projection = "semantic_document.v1";
        String evaluation = "eager_left_to_right";
        int indexBase = 0;
        List<SemanticNode> nodes = new ArrayList<>();

        try (BufferedReader br = Files.newBufferedReader(path)) {
            String line;
            while ((line = br.readLine()) != null) {
                line = line.trim();
                if (line.startsWith("projection=")) {
                    projection = unquote(line.substring("projection=".length()));
                } else if (line.startsWith("evaluation=")) {
                    evaluation = unquote(line.substring("evaluation=".length()));
                } else if (line.startsWith("index_base=")) {
                    indexBase = Integer.parseInt(line.substring("index_base=".length()).trim());
                } else {
                    if (line.startsWith(",")) {
                        line = line.substring(1);
                    }
                    SemanticNode node = parseNode(line);
                    if (node != null) {
                        nodes.add(node);
                    }
                }
            }
        }
        return new SemanticProgram(projection, evaluation, indexBase, nodes);
    }

    private static SemanticNode parseNode(String line) {
        String q = Character.toString(34);
        String idMarker = q + "id" + q + ":";
        String kindMarker = q + "structural_kind" + q + ":" + q;
        String fieldsMarker = q + "fields" + q + ":{";

        int idPos = line.indexOf(idMarker);
        int kindPos = line.indexOf(kindMarker);
        int fieldsPos = line.indexOf(fieldsMarker);
        if (idPos < 0 || kindPos < 0 || fieldsPos < 0) return null;

        int idStart = idPos + idMarker.length();
        int idEnd = line.indexOf(',', idStart);
        if (idEnd < 0) return null;

        int kindStart = kindPos + kindMarker.length();
        int kindEnd = line.indexOf((char)34, kindStart);
        if (kindEnd < 0) return null;

        long id = Long.parseLong(line.substring(idStart, idEnd).trim());
        String structuralKind = line.substring(kindStart, kindEnd);

        int fieldsStart = fieldsPos + fieldsMarker.length();
        int fieldsEnd = line.lastIndexOf('}');
        if (fieldsEnd <= fieldsStart) {
            return new SemanticNode(id, structuralKind, Map.of());
        }

        String fieldsText = line.substring(fieldsStart, fieldsEnd);
        Map<String, String> fields = new LinkedHashMap<>();
        readField(fieldsText, "kind", fields);
        readField(fieldsText, "name", fields);
        readField(fieldsText, "scope_id", fields);
        return new SemanticNode(id, structuralKind, fields);
    }

    private static void readField(String text, String key, Map<String, String> out) {
        String q = Character.toString(34);
        String marker = q + key + q + ":";
        int pos = text.indexOf(marker);
        if (pos < 0) return;

        int start = pos + marker.length();
        while (start < text.length() && Character.isWhitespace(text.charAt(start))) start++;
        if (start >= text.length()) return;

        if (text.charAt(start) == 34) {
            int end = text.indexOf((char)34, start + 1);
            if (end > start) out.put(key, text.substring(start + 1, end));
            return;
        }

        int end = start;
        while (end < text.length() && Character.isDigit(text.charAt(end))) end++;
        if (end > start) out.put(key, text.substring(start, end));
    }

    private static String unquote(String s) {
        s = s.trim();
        if (s.length() > 1 && s.charAt(0) == 34 && s.charAt(s.length() - 1) == 34) {
            return s.substring(1, s.length() - 1);
        }
        return s;
    }
}
