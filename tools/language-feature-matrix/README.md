# Sprachmerkmale und tatsächlich geprüfte Zwischenrouten

Aus dem Projektstamm ausführen. Diese Werkzeuge bauen keine auslieferbare
Transpiler-EXE und verändern weder das Eingabeprogramm noch dessen Messwerte.

```powershell
node tools/language-feature-matrix/build.mjs
node tools/language-feature-matrix/verify-source.mjs
$env:ROUTE_LIVE_SOURCE = '1'
node tools/language-feature-matrix/routes.mjs
node tools/language-feature-matrix/find-routes.mjs python go arithmetic
node tools/language-feature-matrix/find-routes.mjs python go arithmetic,discarded_argument
```

`build.mjs` projiziert die V16-Fallklassen auf 64 Sprachmerkmale. Ergebnis:
`outputs/language-feature-matrix-v1/language_features.json` und die lesbare
`SPRACHMERKMALE.md`. Quell- und Zielbeobachtungen, Modelle, Teilnachweise und
vollständige Semantik sind getrennt. Fehlende Nachweise bedeuten UNKNOWN und
nicht, dass eine Sprache ein Merkmal grundsätzlich nicht besitzt.

`verify-source.mjs` vergleicht alle 9.204 erzeugten Zieltexte direkt aus dem
aktuellen Go-Quellcode mit der nativen V16-Messung und prüft, dass die vorhandene
Release-EXE unverändert bleibt. Der Go-Runner nutzt temporäre Werkzeuge;
es wird keine neue Release-Datei erzeugt.

`routes.mjs` zählt mit A² zunächst mögliche Wege. Anschließend wird der tatsächlich
erzeugte Zwischentext geparst und erneut übersetzt. Die Endausgabe wird gegen den
ursprünglichen Quellvertrag geprüft. Native V16-Ergebnisse dürfen nur bei exakt
gleichem Zieltext und gleicher Zielsprache wiederverwendet werden. Eine neue
Endausgabe wird nativ kompiliert und ausgeführt. Die Tests werden in
`.audit-cache/relay-native` gespeichert; das sind Prüfprogramme, keine neue
CodeTranspiler-EXE.

Standard: alle 59 ausführbaren Fallklassen, alle 156 gerichteten Sprachpaare,
je elf verschiedene Zwischensprachen: 101.244 Kombinationszellen. Identitätswege
werden nicht als zusätzliche Übersetzungen mitgezählt. Parser- oder
Compilerfehler sind FAIL, fehlende Compiler und Ressourcenlimits UNKNOWN.
Der historische Standard dieses V16-Routenwerkzeugs beträgt 2.048 Token pro
Zwischenprogramm. Im damaligen Stand brauchten die Graphen quadratischen Speicher.
Ein Ressourcengrenzfall
wurde nicht bis zum Übersetzungsergebnis geprüft und darf nicht als FAIL gelten.

Um einen kleineren reproduzierbaren Pilotlauf zu messen:

```powershell
$env:ROUTE_FEATURES = 'arithmetic,discarded_argument'
$env:ROUTE_LIVE_SOURCE = '1'
node tools/language-feature-matrix/routes.mjs outputs/transpiler-audit-v16 outputs/language-route-matrix-pilot
Remove-Item Env:ROUTE_FEATURES
```

`find-routes.mjs` bildet das Skalarprodukt eines expliziten Anforderungsvektors
mit den geprüften Pfadmasken. Es liefert alle gleichwertigen Zwischenwege, die
für sämtliche gewählten Beispielverträge bestanden haben, sowie Fehler- und
Unbekanntvektoren. Unbekannte Vertrags-IDs bleiben unbekannt. Dieses Ergebnis
berechtigt nicht dazu, beliebige neue Programme ungeprüft umzuleiten.

Wichtige Grenze: Zwei erfolgreiche Einzelkanten sind kein Nachweis für deren
Zusammensetzung. Beispielsweise kann Python → R → Ziel die Effekte unbenutzter
Argumente verlieren. Solche Wege werden anhand des ursprünglichen Vertrags
abgewiesen. Ein Umweg über R ersetzt keine allgemeine Promise-/Closure-Semantik.

Auch Sprachmerkmale können als Anforderungen angegeben werden:

```powershell
node tools/language-feature-matrix/find-routes.mjs python go feature:arithmetic
node tools/language-feature-matrix/find-routes.mjs python go feature:loop_break,arithmetic
node tools/language-feature-matrix/find-routes.mjs python go feature:closure_environment
```

`feature:NAME` wird anhand der Sprachmerkmalsmatrix in sämtliche zugeordneten
Beispielverträge aufgefächert. Die Messdatei-Prüfsumme beider Matrizen muss
übereinstimmen. Damit bedeutet `feature:arithmetic` beispielsweise mehr als der
einzelne Vertrag `arithmetic`: Auch Klammerung und skalare Negation werden
verlangt. Es gibt keine zusätzliche Gewichtung; doppelte Verträge zählen einmal.

Ein unbekanntes Merkmal oder ein Merkmal ohne Beispielverträge bleibt als
UNKNOWN-Anforderung bestehen. Eine leere Zuordnung gibt niemals alle Wege frei.
In einem Pilotlauf nicht gemessene Verträge bleiben ebenfalls UNKNOWN. Das
Ergebnis ist ein Teilnachweis für die Beispiele, keine Freigabe sämtlicher
Semantik eines Sprachmerkmals oder seiner Kombination mit anderen Merkmalen.

Optional können nach den Anforderungen das Routenverzeichnis und anschließend
das Sprachmerkmalsverzeichnis angegeben werden. Bei Routen aus dem aktuellen
Quellcode zeigt die Abfrage getrennte Quellversionen und einen Hinweis zur
gespeicherten `source_equivalence.json`, sofern sie im Routenverzeichnis liegt.
Diese Herkunftsangaben sind keine neue Übersetzungs- oder Laufzeitprüfung.

## Gemeinsamer Quellstand V17

```powershell
node tools/language-feature-matrix/joint.mjs outputs/semantic-matrix-v17
node tools/language-feature-matrix/dependencies.mjs outputs/semantic-matrix-v17
```

Der zuletzt vollständig geprüfte Quellstand dieses Blocks liegt unter
`outputs/semantic-matrix-v17-final`. Dort prüft `verify.mjs` die gespeicherten
Ergebnisse nochmals gegen die Originalverträge, den aktuellen Quellhash und
die unveränderte EXE. `JOINT_REUSE_DIR` erlaubt die Wiederverwendung gespeicherter
nativer PASS-Belege ausschließlich nach Prüfung von Zielsprache und Codehash;
die Herkunftsdateien werden mit ihren Hashes im Bericht festgehalten.

`joint.mjs` übersetzt sämtliche Originalbeispiele mit dem aktuellen Quellstand,
liest jeden unterschiedlichen tatsächlichen Zwischentext einmal und erzeugt
daraus die nächsten Ziele. Das erneute Lesen verwendet keinen ursprünglichen
AST und keine im Zielcode versteckte Quellkopie. Direktwege, Rückwege und Wege
über eine dritte Sprache werden gegen denselben ursprünglichen Ausgabevertrag
geprüft. Native Belege dürfen nur bei identischer Sprache und identischen
Codebytes wiederverwendet werden. `JOINT_FEATURES=arithmetic,discarded_argument`
begrenzt den Lauf auf den Pilotumfang; ohne diese Variable werden alle 59
ausführbaren Fallklassen geprüft. Die 16 reservierten Klassen bleiben unbewiesen
und sind nicht im Nenner des ausführbaren V17-Korpus enthalten.

Der Grenzwert beträgt hier 65.536 Token. Beziehungsmatrizen und Klammerpaare
sind dünn besetzt; kleine Merkmals- und Zustandsmatrizen bleiben dicht. Ein
mathematisch dichter Erreichbarkeitsabschluss kann weiterhin quadratisch groß
werden. Änderungen während der Messung werden über den Quellmanifest-Hash
abgewiesen, Änderungen der vorhandenen EXE ebenso.

`dependencies.mjs` berechnet aus den tatsächlichen AST-Knoten die Projektion
`B = X * W`, den gemeinsamen Abhängigkeitsabschluss und die dünn besetzten
Pfadmatrizen. Anforderungen und diagnostische Fehlerkandidaten sind getrennt.
Mehrfachzuordnungen dürfen nicht als zusätzliche Fehler gezählt werden. Alle
Gewichte sind 1; es wird keine Prioritätenrangfolge erzeugt.

Der AST trägt jetzt den ausführbaren Baum sowie Auswertungs-, Werte- und
Indexvertrag. Er ersetzt die ausschließliche Weitergabe eines R-Textes. Die
vorhandenen konservativen Quellparser sind weiterhin nötig. Der Decoder für
erzeugten Code gleicht Laufzeitpräfix und wiedererzeugte Programm-Token ab;
unbekannte Funktionshelfer und Zustandsmaschinen werden nicht still übergangen.
Explizite R-Aufrufwrapper verwenden echte `force`-Operationen und werden nur
bei passender tatsächlicher Helferstruktur zurückübersetzt.

Die neue Messung liegt in einem eigenen Schema. `find-routes.mjs` liest weiter
das historische V16-Routenschema und darf nicht auf die V17-Datei umgebogen
werden. Weder `joint.mjs` noch `dependencies.mjs` bauen eine Release-EXE.
