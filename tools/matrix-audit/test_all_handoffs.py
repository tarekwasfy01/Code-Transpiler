import unittest

from all_handoffs import normalize, project


class JointMatrixTests(unittest.TestCase):
    def test_named_alignment_and_missing_features(self):
        columns, values, missing = project({"b": 3, "a": 2, "dialect": 5},
            [{"feature": "a", "node": "2", "relation": "1"},
             {"feature": "b", "node": "1", "relation": "0"}], "feature")
        self.assertEqual(columns, ["node", "relation"])
        self.assertEqual(values, [7, 2])
        self.assertEqual(missing, ["dialect"])

    def test_equal_language_normalization(self):
        self.assertEqual(normalize({"x": 200, "y": 100}), normalize({"x": 2, "y": 1}))
        for values in ({"x": 0}, {"x": -1}, {"x": float("nan")}):
            with self.assertRaises(ValueError):
                normalize(values)

    def test_duplicate_projection_rows_rejected(self):
        with self.assertRaises(ValueError):
            project({"a": 1}, [{"f": "a", "n": 1}, {"f": "a", "n": 0}], "f")


if __name__ == "__main__":
    unittest.main()
