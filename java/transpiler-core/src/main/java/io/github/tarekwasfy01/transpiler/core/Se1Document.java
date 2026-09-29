package io.github.tarekwasfy01.transpiler.core;
/** Read-only SE/1 exchange document used by the bootstrap ports. */
public record Se1Document(String sourceLanguage, String canonicalLanguage, String semanticsJson, String juliaPivot) {
    public Se1Document {
        sourceLanguage = sourceLanguage == null ? "" : sourceLanguage;
        canonicalLanguage = canonicalLanguage == null ? "" : canonicalLanguage;
        semanticsJson = semanticsJson == null ? "" : semanticsJson;
        juliaPivot = juliaPivot == null ? "" : juliaPivot;
    }
}
