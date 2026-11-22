"""Oracle self-tests. Helpers here stay outside the agent's product workspace."""
import heapq
import importlib.util
from pathlib import Path
import tempfile
import unittest


PATH = Path(__file__).resolve().parents[1] / "cases" / "relay-station" / "product_checks.py"
SPEC = importlib.util.spec_from_file_location("relay_product_checks", PATH)
product = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(product)


class RouteAPI:
    """Small independent Dijkstra implementation for checking generated route expectations."""
    def __init__(self, reverse_tie=False):
        self.edges = {}
        self.reverse_tie = reverse_tie

    def call(self, operation, error=None, **data):
        if operation == "import":
            for edge in data["links"]:
                self.edges.setdefault(edge["from"], []).append((edge["to"], edge["minutes"]))
            return {"ok": True}
        assert operation == "route"
        queue = [(0, 0, (data["origin"],))]
        while queue:
            cost, hops, route = heapq.heappop(queue)
            if route[-1] == data["destination"]:
                assert error is None, route
                if self.reverse_tie and route == ("A", "B", "Z", "T"):
                    route = ("A", "C", "M", "T")  # same cost and length, wrong full-path tie-break
                return {"ok": True, "minutes": cost, "path": list(route)}
            for site, minutes in self.edges.get(route[-1], []):
                if site not in route:
                    heapq.heappush(queue, (cost + minutes, hops + 1, route + (site,)))
        assert error == "unreachable"
        return {"ok": False, "error": error}


class RelayProductTests(unittest.TestCase):
    def test_independent_route_solver_and_equal_cost_mutant(self):
        product.routing(RouteAPI())
        with self.assertRaisesRegex(AssertionError, "route A -> T.path"):
            product.routing(RouteAPI(reverse_tie=True))

    def test_rejects_bool_instead_of_inventory_count(self):
        with self.assertRaises(AssertionError):
            product.expect({"used": True}, {"used": 1}, "busy")

    def test_empty_implementation_cannot_pass_public_checks(self):
        with tempfile.TemporaryDirectory() as blank:
            rows = product.product_checks(Path(blank))
        self.assertEqual({row["id"] for row in rows}, {"network", "routing", "lifecycle", "ledger", "recovery"})
        self.assertTrue(all(row["status"] == "failed" for row in rows), rows)

    def test_subprocess_boundary_and_exit_codes(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            package = root / "relay"
            package.mkdir()
            (package / "__main__.py").write_text(
                'import json,sys\n'
                'assert sys.argv[1] == "--db" and sys.argv[3] == "route"\n'
                'assert json.load(sys.stdin) == {"origin":"A","destination":"B"}\n'
                'print(json.dumps({"ok":False,"error":"unreachable"}))\n'
                'sys.exit(2)\n')
            client = product.Client(root, root / "业务 文件.sqlite")
            client.call("route", error="unreachable", origin="A", destination="B")
            with self.assertRaisesRegex(AssertionError, "expected exit 0"):
                client.call("route", origin="A", destination="B")
