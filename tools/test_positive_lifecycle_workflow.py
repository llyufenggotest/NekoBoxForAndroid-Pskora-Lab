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
        self.assertIn('with_gvisor,with_quic,with_wireguard,with_utls,with_clash_api', block)
        self.assertIn('with_gvisor,with_quic,with_wireguard,with_utls,with_clash_api,with_low_memory', block)
        for package in (
            'github.com/sagernet/sing-tun',
            'github.com/sagernet/sing-box/experimental/clashapi',
            'github.com/sagernet/sing-box/common/urltest',
        ):
            self.assertEqual(2, block.count(package))
        self.assertNotIn('|| true', block)
        self.assertNotIn('continue-on-error', block)
        self.assertLess(match.start(), text.index('      - name: Prepare private Oix'))


if __name__ == '__main__':
    unittest.main()
