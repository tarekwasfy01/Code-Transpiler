# Code Transpiler — Java / Maven

Native Java/Maven bootstrap of Code Transpiler's semantic IR and SE/1 projection.

## Maven coordinates

```xml
<dependency>
  <groupId>io.github.tarekwasfy01</groupId>
  <artifactId>code-transpiler-core</artifactId>
  <version>0.2.0</version>
</dependency>
```

The CLI artifact is published as `io.github.tarekwasfy01:code-transpiler-cli`. Maven also builds a shaded standalone `*-all.jar`.

## Build

```bash
cd java
mvn -B -ntp verify
```

Requires Java 21.

## CLI

```bash
java -jar code-transpiler-cli-0.2.0-all.jar routes go java
java -jar code-transpiler-cli-0.2.0-all.jar inspect-se input.se
java -jar code-transpiler-cli-0.2.0-all.jar transpile-se input.se GeneratedProgram.java GeneratedProgram
```

## GitHub Packages release

The workflow `.github/workflows/maven-publish.yml` publishes both Maven artifacts to GitHub Packages. Push a tag such as `v0.2.0`, or run the workflow manually and supply a version.

The workflow uses the repository-provided `GITHUB_TOKEN` with `packages: write`.

## License

MIT License — Copyright (c) 2026 Tarek Wasfy.

See [LICENSE](LICENSE).
