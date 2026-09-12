import importlib.util
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("verify_maintenance_ci", ROOT / "tools" / "verify_maintenance_ci.py")
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class MaintenanceCIContractTest(unittest.TestCase):
    def test_workflow_contract(self):
        workflow = ROOT / ".github" / "workflows" / "maintenance-core115.yml"
        self.assertEqual([], MODULE.verify(ROOT, workflow))


if __name__ == "__main__":
    unittest.main()
