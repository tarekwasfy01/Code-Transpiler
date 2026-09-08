# Third-party notices

R2Many GUI uses Gio and gvcode. Backend source was consolidated from the R2Go, R2Rust, R2Cpp and R2Py project family and expanded with additional target generators.

Tree-sitter assets and runtime-derived parser data are distributed under the MIT
License. The complete notice is embedded in the executable and is available
with `CodeTranspiler.exe licenses` (also `licences`). The source copy is kept at
`licenses/TreeSitterLicense.txt`.

Nim 2.2.10 is used as a local validation toolchain for Matrix V15. It is not
bundled into CodeTranspiler.exe and no system installer or PATH modification is
performed. The official Windows x64 archive is linked from
https://nim-lang.org/install.html and supplied by nim-lang/nightlies on GitHub.
Archive SHA-256: fe0686a9b298e5b13d0a983df37e002a8c6320f8b16cc45a51d15cf4046a109f.
The compiler distribution's MIT notice (copyright 2006-2026 Andreas Rumpf) is
preserved in its copying.txt and in outputs/transpiler-audit-v15/nim-toolchain-license.txt.
The generated Nim support code is project source and imports Nim's standard
library when an external Nim compiler builds a translated program.

## py2many

The empirical Python-to-target emitter contracts used during backend
validation were derived from the local py2many project. py2many is distributed
under the MIT License. Its source is not a runtime dependency of
CodeTranspiler and is not executed by the product. The upstream project is
available at https://github.com/py2many/py2many.

Copyright (c) 2015 Lukas Martinelli
Copyright (c) 2019 Julian Konchunas
Copyright (c) 2020 Arun Sharma

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
