# CrossTL design boundary

SemanticProgram uses no copied CrossTL source code. Its local Go registry and
visitor/traversal APIs were independently implemented after reviewing CrossTL's
public architecture.

CrossTL is a shader and compute translator around CrossGL. It is a useful
future external GPU-dialect adapter, rather than the core of this general
program IR. A future adapter must exchange a versioned GPU dialect document,
declare resource/stage/capability requirements and reject unsupported targets
explicitly. It must not silently lower GPU resource semantics into CPU values.

The first concrete boundary is present in `SemanticProgram.dialects`:

```json
{"name":"gpu","capabilities":["gpu.compute"],"operations":[
  {"id":"kernel-1","kind":"compute","attributes":{"workgroup_size":[64,1,1]}}
]}
```

Current CPU backends reject that document with a capability error. A future
CrossTL adapter can register GPU capabilities only after it has an executable,
versioned CrossGL mapping and its own end-to-end validation.

No CrossTL package, runtime or generated code is redistributed by this project.
If that changes, Apache-2.0 license and NOTICE obligations need a separate
third-party provenance review before shipping.
