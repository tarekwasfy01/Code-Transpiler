import ast
import json
import unittest
import inspect
import itertools
import os
from pathlib import Path
import subprocess

import python_handoff as tool


class MatrixHandoffTests(unittest.TestCase):
    def test_go_binding_against_python_signature_matrix(self):
        cases, expected = [], []
        P = inspect.Parameter
        for optional, variadic, keyword_rest in itertools.product((False, True), repeat=3):
            parameters = [P("x", P.POSITIONAL_ONLY), P("y", P.POSITIONAL_OR_KEYWORD, default=None if optional else P.empty)]
            if variadic:
                parameters.append(P("rest", P.VAR_POSITIONAL))
            parameters.append(P("k", P.KEYWORD_ONLY, default=None if optional else P.empty))
            if keyword_rest:
                parameters.append(P("options", P.VAR_KEYWORD))
            signature = inspect.Signature(parameters)
            modes = {P.POSITIONAL_ONLY: "positional_only", P.POSITIONAL_OR_KEYWORD: "positional_or_keyword", P.VAR_POSITIONAL: "variadic_positional", P.KEYWORD_ONLY: "keyword_only", P.VAR_KEYWORD: "variadic_keyword"}
            for n in range(5):
                for mask in itertools.product((False, True), repeat=4):
                    keywords = {name: 1 for name, present in zip(("x", "y", "k", "z"), mask) if present}
                    cases.append({"parameters": [{"name": p.name, "passing": modes[p.kind], "has_default": p.default is not P.empty} for p in parameters], "arguments": [{"name": ""} for _ in range(n)] + [{"name": k} for k in keywords]})
                    try:
                        bound = signature.bind(*range(n), **keywords)
                        counts = [len(bound.arguments.get(p.name, ())) if p.kind in (P.VAR_POSITIONAL, P.VAR_KEYWORD) else int(p.name in bound.arguments) for p in parameters]
                        defaults = [int(p.name not in bound.arguments and p.default is not P.empty) for p in parameters]
                        expected.append((counts, defaults))
                    except TypeError:
                        expected.append(None)
        root = Path(__file__).resolve().parents[2]
        env = dict(os.environ, GOCACHE=str(root / ".audit-cache/go-build"))
        proc = subprocess.run(["go", "run", "./cmd/signature-bind"], cwd=root, env=env, input=json.dumps(cases), text=True, capture_output=True, timeout=120)
        self.assertEqual(proc.returncode, 0, proc.stderr)
        actual = json.loads(proc.stdout)
        self.assertEqual(len(actual), len(expected))
        for i, (result, oracle) in enumerate(zip(actual, expected)):
            if oracle is None:
                self.assertIn("error", result, f"case {i}")
            else:
                self.assertNotIn("error", result, f"case {i}: {result}")
                self.assertEqual(result["binding"]["argument_counts"], oracle[0], f"case {i}")
                self.assertEqual(result["binding"]["use_defaults"], oracle[1], f"case {i}")
        print(f"Signature differential matrix: {len(cases)} cases PASS", flush=True)

    def test_products_and_basis(self):
        self.assertEqual(tool.matvec([[1, 2], [3, 4]], [2, 3]), [8, 18])
        with self.assertRaises(ValueError):
            tool.matvec([[1]], [2, 3])
        m = tool.incidence([["class", "class"], ["class", "import"]], ["class", "import"])
        self.assertEqual(tool.transpose_product_vector(m, [2, 3]), [5, 3])

    def test_signature_preserves_required_and_explicit_none(self):
        tree = ast.parse("def f(a: int, /, b=None, *args: str, required, optional=None, **kwargs): pass")
        report = tool.declarations(tree)
        function = next(d for d in report["declarations"] if d["kind"] == "FunctionDef")
        params = {p["name"]: p for p in function["parameters"]}
        self.assertEqual(list(params), ["a", "b", "args", "required", "optional", "kwargs"])
        self.assertEqual(params["a"]["passing"], "positional_only")
        self.assertFalse(params["a"]["has_default"])
        self.assertTrue(params["b"]["has_default"])
        self.assertFalse(params["required"]["has_default"])
        self.assertTrue(params["optional"]["has_default"])
        self.assertEqual(params["args"]["passing"], "variadic_positional")
        self.assertEqual(params["kwargs"]["passing"], "variadic_keyword")
        self.assertEqual(report, json.loads(json.dumps(report)))
        self.assertEqual(sum(report["kind_counts"]), len(report["declarations"]))

    def test_class_import_and_generic_capture_without_execution(self):
        tree = ast.parse("from ..data import Thing as Alias\nclass Box[T: int](Base, metaclass=Meta):\n value: T\n def get(self) -> T: return self.value\n")
        report = tool.declarations(tree)
        cls = next(d for d in report["declarations"] if d["kind"] == "ClassDef")
        method = next(d for d in report["declarations"] if d["kind"] == "FunctionDef")
        imp = next(d for d in report["declarations"] if d["kind"] == "ImportFrom")
        self.assertEqual(imp["level"], 2)
        self.assertEqual(imp["names"][0]["fields"]["asname"], "Alias")
        self.assertEqual(cls["type_params"][0]["kind"], "TypeVar")
        self.assertEqual(method["owner"], cls["id"])
        self.assertFalse(report["executable"])
        self.assertEqual(report, tool.declarations(tree))


if __name__ == "__main__":
    unittest.main()
