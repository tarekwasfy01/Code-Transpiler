# Third-party notices

R2Many GUI uses Gio and gvcode. Backend source was consolidated from the R2Go, R2Rust, R2Cpp and R2Py project family and expanded with additional target generators.

Nim 2.2.10 is used as a local validation toolchain for Matrix V15. It is not
bundled into CodeTranspiler.exe and no system installer or PATH modification is
performed. The official Windows x64 archive is linked from
https://nim-lang.org/install.html and supplied by nim-lang/nightlies on GitHub.
Archive SHA-256: fe0686a9b298e5b13d0a983df37e002a8c6320f8b16cc45a51d15cf4046a109f.
The compiler distribution's MIT notice (copyright 2006-2026 Andreas Rumpf) is
preserved in its copying.txt and in outputs/transpiler-audit-v15/nim-toolchain-license.txt.
The generated Nim support code is project source and imports Nim's standard
library when an external Nim compiler builds a translated program.
