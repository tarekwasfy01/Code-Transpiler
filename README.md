# Code Transpiler — Java / Gradle

Native Java implementation of **Code Transpiler**, built with **Gradle** and published through **GitHub Packages**.

## Requirements

- Java 21
- Gradle 8.x

## Project structure

```text
java/
├── build.gradle
├── settings.gradle
├── transpiler-core/
│   ├── build.gradle
│   └── src/
└── transpiler-cli/
    ├── build.gradle
    └── src/
```

The Gradle build uses the same Java sources as the Maven version of Code Transpiler.

## Modules

### Core

```text
io.github.tarekwasfy01:code-transpiler-gradle-core:1.0.0
```

Contains the native Java transpiler core, Semantic/SE handling, bootstrap routing and Java transpilation logic.

### CLI

```text
io.github.tarekwasfy01:code-transpiler-gradle-cli:1.0.0
```

Contains the command-line interface.

The CLI build also produces a standalone fat JAR with the classifier:

```text
all
```

Example:

```text
code-transpiler-gradle-cli-1.0.0-all.jar
```

## Build with Gradle

From the `java` directory:

```bash
gradle clean build
```

To also build the standalone CLI JAR:

```bash
gradle clean build fatJar
```

## Run the CLI

After building:

```bash
java -jar transpiler-cli/build/libs/code-transpiler-gradle-cli-1.0.0-all.jar
```

Available commands include:

```text
routes <source> <target>

inspect-se <file.se>

transpile-se <input.se> <output.java> [ClassName]
```

Example:

```bash
java -jar transpiler-cli/build/libs/code-transpiler-gradle-cli-1.0.0-all.jar routes go java
```

## GitHub Packages

The Gradle artifacts are published to the GitHub Maven package registry:

```text
https://maven.pkg.github.com/tarekwasfy01/Code-Transpiler
```

Publishing is handled by:

```text
.github/workflows/gradle-publish.yml
```

The workflow uses:

- Java 21
- Gradle 8.10.2
- `GITHUB_TOKEN`
- `packages: write`

No GitHub Release is required.

## Publish version 1.0.0

Open:

```text
GitHub
→ Actions
→ Gradle Package
→ Run workflow
```

Use:

```text
version: 1.0.0
```

The workflow runs:

```bash
gradle --no-daemon -PreleaseVersion=1.0.0 clean build fatJar
```

and then:

```bash
gradle --no-daemon -PreleaseVersion=1.0.0 publish
```

## Using the package

Because the package is hosted on GitHub Packages, add the GitHub Maven registry to your Gradle project.

```gradle
repositories {
    maven {
        url = uri("https://maven.pkg.github.com/tarekwasfy01/Code-Transpiler")

        credentials {
            username = System.getenv("GITHUB_ACTOR")
            password = System.getenv("GITHUB_TOKEN")
        }
    }
}
```

### Core dependency

```gradle
dependencies {
    implementation "io.github.tarekwasfy01:code-transpiler-gradle-core:1.0.0"
}
```

### CLI dependency

```gradle
dependencies {
    implementation "io.github.tarekwasfy01:code-transpiler-gradle-cli:1.0.0"
}
```

## Maven coordinates

### Core

```text
Group:    io.github.tarekwasfy01
Artifact: code-transpiler-gradle-core
Version:  1.0.0
```

### CLI

```text
Group:    io.github.tarekwasfy01
Artifact: code-transpiler-gradle-cli
Version:  1.0.0
```

## License

MIT License

Copyright (c) 2026 Tarek Wasfy

See the repository `LICENSE` file for the full license text.

## Repository

Code Transpiler:

https://github.com/tarekwasfy01/Code-Transpiler
