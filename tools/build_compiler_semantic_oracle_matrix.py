#!/usr/bin/env python3
"""Compile the local compiler-semantic oracle into UAST/backend evidence matrices.

This is deliberately an evidence join, not a second IR and not a source parser.
It uses compiler-node definitions as an oracle, UPI's canonical construct
contracts as the UAST mapping, and the concrete Go execution-handler registry
as the implementation witness.
"""
from __future__ import annotations

import argparse
import csv
import re
from collections import defaultdict
from pathlib import Path


# The entries describe semantic *effects*, not spelling in a source language.
# They are evaluated against compiler-node definitions / IR opcode names from
# the oracle.  Ordering matters only where a compound operation is more exact.
SEMANTICS = [
    ("INDEX_SLICE", r"array(subscript|section)|index(expr|ed)|slice(expr|d)?|subscript|slicing", "Index;Slice", "execData"),
    ("AGGREGATE", r"(aggregate|composite|collection|array|tuple|list|dictionary|dict|record|struct|map)(expr|lit|init|type)?", "AggregateCollection;Tuple;StructRecord;MapDictionaryType;ArraySliceType", "execData"),
    ("CLOSURE_FUNCTION", r"(lambda|closure|func(lit|tion)?|function(value|literal)?|blockexpr)", "ClosureLambda;FunctionDeclaration;MethodDeclaration", "execCall"),
    ("CALL", r"(call(expr)?|invoke|apply|message(send)?|methodcall)", "Call", "execCall"),
    ("MEMBER_ACCESS", r"(member|selector|field(access|expr)?|property|qualified)", "MemberAccess", "execData"),
    ("CONVERSION", r"(cast|convert|coerce|reinterpret|asexpr|typeassert)", "CastConversion;TypeAssertTest", "execConversion"),
    ("BINARY_UNARY_OPERATOR", r"(binary|unary|operator|infix|prefix|postfix|comparison|logical|arithmetic)", "OperatorExpression", "execExpression"),
    ("LITERAL", r"(literal|constant(value)?|basiclit|integer|float|string|boolean|nil|none|null)", "Literal;ConstantDeclaration", "execExpression"),
    ("BINDING_DECLARATION", r"(variable|binding|declarator|declaration|letstmt|vardecl|identifier)", "VariableDeclaration;ConstantDeclaration;Parameter", "execBinding"),
    ("ASSIGNMENT", r"(assign|store|setvalue|write)", "Assignment", "execBinding"),
    ("ITERATION", r"(foreach|for(stmt|loop)?|while|do(while|stmt)?|range|iterate|comprehension)", "ForeachEnhancedFor;ForLoop;WhileLoop;DoRepeatWhile;Range;Comprehension", "execControl"),
    ("CONDITIONAL", r"(if(stmt|expr)?|conditional|ternary|select|branch)", "IfConditional;ConditionalTernary;Select", "execControl"),
    ("SWITCH_MATCH", r"(switch|match|when|case)", "SwitchMatchWhen", "execControl"),
    ("CONTROL_TRANSFER", r"(return|yield|break|continue|goto|label)", "Return;Yield;Break;Continue;GotoLabel", "execControl"),
    ("EXCEPTION", r"(try|catch|throw|raise|panic|finally|except)", "Try;CatchExcept;ThrowRaisePanic;Finally", "execException"),
    ("TYPE", r"(type|generic|interface|trait|protocol|enum|class|optional|nullable|constraint|alias)", "ArraySliceType;MapDictionaryType;TypeAlias;TypeConstraint;GenericInstantiation;GenericParameter;Class;InterfaceTraitProtocol;EnumVariant;OptionalNullable", "execTypes"),
    ("MEMORY", r"(pointer|address|deref|alloc|memory|borrow|unsafe)", "PointerReference;Unsafe", "execMemory"),
    ("CONCURRENCY", r"(async|await|spawn|task|channel|atomic|synchroniz)", "Async;Await;SpawnTask;ChannelSendReceive;SynchronizationAtomic", "execConcurrency"),
    ("MODULE", r"(module|namespace|import|export|use)", "ModuleNamespace;ImportUse", "execModule"),
    ("LIFETIME", r"(ownership|lifetime|defer|cleanup|drop)", "OwnershipLifetime;DeferCleanup", "execLifetime"),
    ("COMPILETIME", r"(macro|preprocess|template|interpolat|compiletime)", "MacroPreprocessor;Interpolation", "execCompileTime"),
    ("ANNOTATION", r"(annotat|attribute|modifier|visibility)", "AnnotationAttribute;VisibilityModifier", "execAnnotation"),
    ("FFI_ABI", r"(foreign|ffi|abi|extern)", "ForeignFFI", "execABI"),
]

# These are compiler/IR operations with a concrete canonical schema target,
# but no corresponding UPI *surface construct*. They are useful evidence for
# backend consumption, while intentionally remaining UASF-unresolved until a
# facet contract exists. The marker prevents a false "missing backend" result.
DIRECT_SCHEMA_SEMANTICS = [
    ("BINDING_REFERENCE", r"(declref|flowcapture(reference)?|getvalue|load(local|fast|deref)?|aload(_\d|_w)?|dload|fload|iload|lload|current_op)", "SymbolRef", "execBinding"),
    ("ALLOCATION", r"(newexpr|objectcreation|alloc(a|ate|ation)?|makeclosure|box|cglobal(_auto)?)", "MemoryManagement", "execMemory"),
    ("DEALLOCATION", r"(deleteexpr|free|drop|release|dealloc)", "DeferCleanupStmt", "execLifetime"),
    ("ASSERTION", r"(assert|checkfun|contractchecks|baseguard)", "IfStmt", "execControl"),
    ("IR_BINARY_OPERATION", r"^(add|sub|mul|div|rem|mod|pow|and|or|xor|shl|shr|ashr|lshr|eq|ne|lt|le|gt|ge|cmp|exp|expt|neg|not|iinc|fneg|dneg|lneg|ineg)$", "OperationExpr", "execExpression"),
    ("IR_CONVERSION", r"(fpext|fptrunc|sitofp|uitofp|fptosi|fptoui|zext|sext|trunc|bitcast|ptrtoint|inttoptr|fnptrtoptr)", "ConvertExpr", "execConversion"),
    ("IR_CONTROL_TRANSFER", r"(br(if)?|jump|falseedge|falseunwind|resume|unreachable|interpreter_exit|endloopcntxt)", "GotoLabelStmt", "execControl"),
    ("IR_EXCEPTION", r"(landingpad|resume|panic|throw|raise|catch|abort|trap)", "RaisePanicStmt", "execException"),
]

# Compiler inventories contain a long tail of spelling variants that carry
# the same canonical contract.  Keep this fallback table deliberately small
# and structural: it folds established AST node families onto existing UAST
# primitives without inventing language-specific semantics.
FAMILY_SEMANTICS = [
    ("BINDING_REFERENCE", r"^(?:[AI]LOAD|[AI]STORE|[IFLDA]LOAD|[IFLDA]STORE|LOAD|STORE|GETVALUE|SETVALUE|DECLREF|NAME)$|(?:LOAD|STORE)_", "SymbolRef", "execBinding"),
    ("AGGREGATE", r"^(?:BUILD_|ANEWARRAY|ARRAY(?:CREATION|INITIALIZER)|COMPOUNDLITERAL|NEWARRAY|TUPLE|LIST|MAP|SET)", "AggregateCollection;Tuple;StructRecord;MapDictionaryType", "execData"),
    ("MEMORY", r"^(?:ALLOC|ALLOCA|ADDRESS|DEREF|POINTER|BOX|BORROW|BEGINACCESS|DEALLOC|FREE|RELEASE)", "PointerReference;MemoryManagement", "execMemory"),
    ("EXCEPTION", r"^(?:ATHROW|THROW|TRAP|ABORT|PANIC|LANDINGPAD|RESUME)", "RaisePanicStmt", "execException"),
    ("CONTROL_TRANSFER", r"^(break|continue|yield|return|goto|unreachable|resume|throw|raise).*", "Return;Break;Continue;Yield", "execControl"),
    ("CONDITIONAL", r"(conditional|branch|if|choose|select|switch|case|match)", "IfConditional;Select;SwitchMatchWhen", "execControl"),
    ("ITERATION", r"(loop|for|while|range|iterate|comprehension|arrayinitloop)", "ForLoop;WhileLoop;Range;Comprehension", "execControl"),
    ("TYPE", r"(type|decltype|typeof|qualtype|attributed|pointer|record|struct|class|interface|enum|functiontype)", "ArraySliceType;MapDictionaryType;TypeAlias;GenericInstantiation", "execTypes"),
    ("MODULE", r"^(import|export|module|namespace|using|use|include)", "ModuleNamespace;ImportUse", "execModule"),
    ("LITERAL", r"(literal|constant|const|integer|float|double|string|character|boolean|bool|null|none)", "Literal;ConstantDeclaration", "execExpression"),
    ("BINDING_DECLARATION", r"(decl|declaration|parameter|variable|global|name|identifier|binding)", "VariableDeclaration;ConstantDeclaration;Parameter", "execBinding"),
    ("ASSIGNMENT", r"(store|assign|compoundassign|setvalue)", "Assignment", "execBinding"),
    ("CALL", r"(call|invoke|apply|message|coawait)", "Call", "execCall"),
    ("AGGREGATE", r"(array|tuple|list|dict|map|record|struct|compound|aggregate|initializer)", "AggregateCollection;Tuple;StructRecord;MapDictionaryType", "execData"),
]

INTERNAL_DETAIL = re.compile(
    r"^(cache|extended_arg|nop|dup\d*|dup_x\d+|swap|pop\d*|reserved|coverage(kind)?|"
    r"constevalcounter|edition\d+|interpreter_exit|endloopcntxt|default|built|deep|"
    r"error|invalid|empty|end|the_reference|i|s)$",
    re.IGNORECASE,
)


def clean(v: str) -> str:
    return (v or "").strip()


def has_handler(go: str, execution: str) -> tuple[bool, str]:
    # A registry line is the productive witness. Validators alone are not
    # treated as a backend implementation unless registered in this table.
    needle = execution + ":"
    for line in go.splitlines():
        if needle in line and "{" in line:
            return True, line.strip()
    return False, ""


def semantic_for(row: dict[str, str]) -> tuple[str, str, str, str]:
    evidence = " ".join((clean(row.get("raw_primitive")), clean(row.get("evidence")), clean(row.get("source_kind")))).lower()
    for primitive, pattern, constructs, execution in SEMANTICS:
        if re.search(pattern, evidence, re.IGNORECASE):
            return primitive, constructs, execution, "STRUCTURALLY_EVIDENCED"
    # Match the normalized compiler operation identifier independently from
    # source spelling. This covers bytecode/MIR/SSA operations such as ALOAD,
    # AddWithOverflow, and DeclRefExpr, whose semantics are stated by their
    # compiler representation rather than a language-level grammar rule.
    node = clean(row.get("raw_primitive")).lower()
    for primitive, pattern, structure, execution in DIRECT_SCHEMA_SEMANTICS:
        if re.search(pattern, node, re.IGNORECASE):
            return primitive, "@schema:" + structure, execution, "STRUCTURALLY_EVIDENCED_SCHEMA"
    for primitive, pattern, constructs, execution in FAMILY_SEMANTICS:
        if re.search(pattern, node, re.IGNORECASE):
            return primitive, constructs, execution, "STRUCTURALLY_EVIDENCED_FAMILY"
    if INTERNAL_DETAIL.match(node):
        return "COMPILER_IMPLEMENTATION_DETAIL", "", "", "NON_CANONICAL_COMPILER_DETAIL"
    return "UNMAPPED_ORACLE_SEMANTICS", "", "", "INSUFFICIENT_SEMANTIC_EVIDENCE"


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--oracle-root", type=Path, default=Path(r"C:\Users\tarek\Desktop\compiler_semantic_oracle_kit"))
    ap.add_argument("--upi", type=Path, default=Path("matrices/UPI_DIRECT_IMPLEMENTATION_MATRIX.csv"))
    ap.add_argument("--handlers", type=Path, default=Path("internal/backend/uast_execution_primitives.go"))
    ap.add_argument("--package-failures", type=Path, default=Path("outputs/miner-v4.3-all-to-all/failure_matrix.csv"))
    ap.add_argument("--out", type=Path, default=Path("outputs/compiler-semantic-oracle"))
    args = ap.parse_args()
    args.out.mkdir(parents=True, exist_ok=True)

    with (args.oracle_root / "raw_primitive_candidates.csv").open(encoding="utf-8-sig", newline="") as f:
        oracle = list(csv.DictReader(f))
    with args.upi.open(encoding="utf-8-sig", newline="") as f:
        upi = list(csv.DictReader(f))
    handlers = args.handlers.read_text(encoding="utf-8")

    upi_by_construct: dict[str, list[dict[str, str]]] = defaultdict(list)
    for row in upi:
        upi_by_construct[clean(row["construct"])].append(row)

    ir_rows, ir_to_uast, existing, missing, empirical = [], [], [], [], []
    seen_mapping, seen_existing, seen_missing = set(), set(), set()
    observed = defaultdict(int)

    for row in oracle:
        primitive, constructs, execution, evidence_status = semantic_for(row)
        observed[primitive] += 1
        base = {
            "language": clean(row.get("language")), "layer": clean(row.get("layer")),
            "project": clean(row.get("project")), "source_file": clean(row.get("source_file")),
            "source_kind": clean(row.get("source_kind")), "compiler_node": clean(row.get("raw_primitive")),
            "evidence": clean(row.get("evidence")), "canonical_primitive": primitive,
            "evidence_status": evidence_status,
        }
        ir_rows.append(base)
        if evidence_status == "NON_CANONICAL_COMPILER_DETAIL":
            continue
        if primitive == "UNMAPPED_ORACLE_SEMANTICS":
            continue
        matched = False
        for construct in constructs.split(";"):
            if construct.startswith("@schema:"):
                matched = True
                structure = construct.split(":", 1)[1]
                key = (primitive, "", structure, "", execution)
                if key not in seen_mapping:
                    seen_mapping.add(key)
                    ir_to_uast.append({
                        "canonical_primitive": primitive, "upi_construct": "",
                        "uast_structures": structure, "uasf": "",
                        "semantic_axes": "schema-derived; UASF contract unresolved", "relations": "",
                        "fields": "", "execution_primitive": execution,
                        "mapping_status": "CANONICAL_SCHEMA_MAPPING_PARTIAL",
                    })
                continue
            for u in upi_by_construct.get(construct, []):
                matched = True
                key = (primitive, construct, clean(u.get("uast_structures")), clean(u.get("uasf_seed")), execution)
                if key not in seen_mapping:
                    seen_mapping.add(key)
                    ir_to_uast.append({
                        "canonical_primitive": primitive, "upi_construct": construct,
                        "uast_structures": clean(u.get("uast_structures")), "uasf": clean(u.get("uasf_seed")),
                        "semantic_axes": clean(u.get("semantic_axes")), "relations": clean(u.get("uast_relations")),
                        "fields": clean(u.get("uast_fields")), "execution_primitive": execution,
                        "mapping_status": "CANONICAL_UPI_MAPPING",
                    })
        # Do not call it a backend gap when the semantic family has no UPI
        # contract. It is an evidence/mapping gap, requiring human review.
        implemented, witness = has_handler(handlers, execution)
        ekey = (primitive, execution)
        if ekey not in seen_existing:
            seen_existing.add(ekey)
            existing.append({"canonical_primitive": primitive, "execution_primitive": execution,
                             "product_handler_present": str(implemented).lower(), "product_handler_witness": witness,
                             "implementation_status": "PRODUCTIVE_HANDLER_REGISTERED" if implemented else "NO_PRODUCT_HANDLER"})
        if not matched or not implemented:
            reason = "NO_CANONICAL_UPI_MAPPING" if not matched else "NO_PRODUCT_HANDLER"
            mkey = (primitive, execution, reason)
            if mkey not in seen_missing:
                seen_missing.add(mkey)
                missing.append({"canonical_primitive": primitive, "execution_primitive": execution,
                                "gap_kind": reason, "oracle_nodes_observed": observed[primitive],
                                "action": "MAP_TO_EXISTING_UAST_CONTRACT" if not matched else "IMPLEMENT_OR_REGISTER_HANDLER"})

    def write(name: str, rows: list[dict[str, object]]) -> None:
        path = args.out / name
        fields = list(rows[0]) if rows else ["status"]
        with path.open("w", encoding="utf-8", newline="") as f:
            w = csv.DictWriter(f, fieldnames=fields)
            w.writeheader(); w.writerows(rows)

    write("compiler_ir_semantic_matrix.csv", ir_rows)
    write("compiler_ir_to_uast_matrix.csv", ir_to_uast)
    write("primitive_existing_implementation_matrix.csv", existing)
    write("primitive_missing_matrix.csv", missing)

    # Empirical outcomes are useful only after their causal stage has been
    # separated. A source parser / incomplete UAST failure is never evidence
    # for a target-backend gap. Keep its original diagnostic verbatim so this
    # evidence remains auditable instead of inventing semantic primitives.
    if args.package_failures.exists():
        with args.package_failures.open(encoding="utf-8-sig", newline="") as f:
            for row in csv.DictReader(f):
                stage = clean(row.get("first_failure_stage")).upper()
                diagnostic = clean(row.get("compiler_error") or row.get("failure_signature"))
                lower = diagnostic.lower()
                if stage in {"FRONTEND", "PARSER", "UAST_BUILD", "UAST_NORMALIZE"} or any(x in lower for x in (
                    "matrix parser", "expected expression", "unexpected character", "function name missing", "universal node",
                )):
                    causal = "FRONTEND_OR_UAST"
                elif stage in {"TARGET_PARSE", "TARGET_COMPILE", "TARGET_TEST", "BACKEND", "SOURCE_EMIT"}:
                    causal = "TARGET_BACKEND_OR_TOOLCHAIN"
                else:
                    causal = "UNCLASSIFIED_NON_BACKEND_EVIDENCE"
                empirical.append({
                    "case_id": clean(row.get("case_id")), "source_language": clean(row.get("source_language")),
                    "target_language": clean(row.get("target_language")), "first_failure_stage": stage,
                    "causal_layer": causal, "uast_structure": clean(row.get("uast_structure")),
                    "uast_facet": clean(row.get("uast_facet")), "execution_primitive": clean(row.get("execution_primitive")),
                    "operation": clean(row.get("operation")), "diagnostic": diagnostic,
                })
    write("package_miner_semantic_validation.csv", empirical)
    print(f"ORACLE_NODES={len(oracle)}")
    excluded = {"UNMAPPED_ORACLE_SEMANTICS", "COMPILER_IMPLEMENTATION_DETAIL"}
    print(f"CANONICAL_PRIMITIVES={sum(1 for p in observed if p not in excluded)}")
    print(f"UPI_MAPPINGS={len(ir_to_uast)}")
    print(f"PRODUCTIVE_HANDLER_LINKS={sum(r['product_handler_present'] == 'true' for r in existing)}/{len(existing)}")
    print(f"PROVEN_OR_MAPPING_GAPS={len(missing)}")
    print(f"UNMAPPED_ORACLE_NODES={observed['UNMAPPED_ORACLE_SEMANTICS']}")
    print(f"PACKAGE_FAILURE_ROWS={len(empirical)}")
    print(f"EMPIRICAL_BACKEND_CANDIDATES={sum(r['causal_layer'] == 'TARGET_BACKEND_OR_TOOLCHAIN' for r in empirical)}")


if __name__ == "__main__":
    main()
