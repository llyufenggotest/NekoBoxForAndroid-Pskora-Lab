import re
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


class PositiveLifecycleWorkflowContractTest(unittest.TestCase):
    def test_real_tun_race_modes_run_as_root_without_weakening(self):
        text = (ROOT / '.github/workflows/maintenance-core115.yml').read_text(encoding='utf-8')
        match = re.search(
            r'      - name: Positive optimization lifecycle race gates\n(?P<body>.*?)(?=      - )',
            text,
            re.S,
        )
        self.assertIsNotNone(match, 'missing positive lifecycle race gate')
        block = match['body']
        self.assertIn('PYTHONDONTWRITEBYTECODE: "1"', block)
        self.assertIn('-u root env HOME="$RUNNER_TEMP/positive-lifecycle-race-home"', block)
        self.assertEqual(
            2,
            block.count('sudo --preserve-env=CGO_ENABLED,CC,PYTHONDONTWRITEBYTECODE,PATH'),
        )
        self.assertIn('tools/run_positive_lifecycle_race.sh normal', block)
        self.assertIn('tools/run_positive_lifecycle_race.sh low-memory', block)
        self.assertNotIn('|| true', block)
        self.assertNotIn('continue-on-error', block)
        self.assertLess(match.start(), text.index('      - name: Prepare private Oix'))

        runner = (ROOT / 'tools/run_positive_lifecycle_race.sh').read_text(encoding='utf-8')
        self.assertIn("go test -list '^Test'", runner)
        self.assertIn("TestGoKernelPacketOffload", runner)
        self.assertIn('github.com/sagernet/sing-tun', runner)
        self.assertIn('github.com/sagernet/sing-box/experimental/clashapi', runner)
        self.assertIn('github.com/sagernet/sing-box/common/urltest', runner)
        self.assertEqual(2, runner.count('-timeout=120s'))
        self.assertIn('before=("${sing_tun_tests[@]:0:first_offload}")', runner)
        self.assertIn('after=("${sing_tun_tests[@]:first_offload}")', runner)
        self.assertIn('run_sing_tun_group before-offload "${before[@]}"', runner)
        self.assertIn('run_sing_tun_group offload-and-after "${after[@]}"', runner)
        self.assertNotIn('-timeout=121s', runner)
        self.assertNotIn('|| true', runner)
        self.assertNotIn('-skip', runner)


if __name__ == '__main__':
    unittest.main()
