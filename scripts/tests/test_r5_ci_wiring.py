"""Guard the R5 PR event boundary and read-only CI execution model."""
import re
from pathlib import Path
import unittest

import yaml

ROOT = Path(__file__).resolve().parents[2]


def load(name):
    # BaseLoader preserves GitHub's `on` key rather than YAML 1.1 boolean True.
    return yaml.load((ROOT / '.github/workflows' / name).read_text(), Loader=yaml.BaseLoader)


def matches(branch, patterns):
    # These patterns use only literal names and `*` (which cannot cross `/`).
    return any(re.fullmatch(re.escape(p).replace(r'\*', '[^/]*'), branch) for p in patterns)


class R5CIWiringTests(unittest.TestCase):
    def test_pr_base_boundary_is_shared(self):
        expected = ['main', 'work/company-mail-r5-goal-20260930', 'integration/company-mail-r5-*']
        for name in ['company-p0.yml', 'review-source.yml']:
            with self.subTest(workflow=name):
                event = load(name)['on']['pull_request']
                self.assertEqual(event, {'branches': expected})
                for branch in ['main', 'work/company-mail-r5-goal-20260930',
                               'integration/company-mail-r5-20261003',
                               'integration/company-mail-r5-batch2-20261003',
                               'integration/company-mail-r5-batch3-20261003']:
                    self.assertTrue(matches(branch, event['branches']), branch)
                for branch in ['feature/unrelated', 'work/company-mail-r5-other',
                               'integration/other', 'integration/company-mail-r50-batch2',
                               'integration/company-mail-r5-batch2/nested']:
                    self.assertFalse(matches(branch, event['branches']), branch)

    def test_push_remains_main_only_and_no_privileged_event(self):
        ci = load('company-p0.yml')
        source = load('review-source.yml')
        self.assertEqual(set(ci['on']), {'push', 'pull_request'})
        self.assertEqual(ci['on']['push'], {'branches': ['main']})
        self.assertEqual(set(source['on']), {'pull_request'})
        for branch in ['integration/company-mail-r5-batch3-20261003', 'fork/unrelated']:
            self.assertFalse(matches(branch, ci['on']['push']['branches']))

    def test_read_only_checkout_and_exact_source_archive(self):
        for name in ['company-p0.yml', 'review-source.yml']:
            workflow = load(name)
            self.assertEqual(workflow['permissions'], {'contents': 'read'})
            for job in workflow['jobs'].values():
                self.assertNotIn('permissions', job)
                checkout = [s for s in job['steps'] if s.get('uses') == 'actions/checkout@v4']
                self.assertEqual(len(checkout), 1)
                self.assertEqual(checkout[0]['with']['persist-credentials'], 'false')
        source = load('review-source.yml')['jobs']['source']
        self.assertEqual(source['steps'][0]['with']['ref'], '${{ github.event.pull_request.head.sha }}')

    def test_existing_jobs_toolchain_and_timer_mode(self):
        jobs = load('company-p0.yml')['jobs']
        self.assertEqual(set(jobs), {'backend', 'backend-race',
                                     'frontend', 'frontend-build', 'production-web', 'browser-journey'})
        self.assertNotIn('strategy', jobs['backend-race'])
        self.assertNotIn('needs', jobs['backend'])
        self.assertIn('--tmpfs /var/lib/postgresql/data:', jobs['backend-race']['services']['postgres']['options'])
        for name, job in jobs.items():
            self.assertEqual(job['timeout-minutes'], '15' if name in ('frontend', 'frontend-build') else '20')
            for step in job['steps']:
                if step.get('uses') == 'actions/setup-go@v5':
                    self.assertEqual(step['with'], {'go-version-file': 'go.mod'})
        for name in ['backend', 'backend-race', 'browser-journey']:
            self.assertEqual(jobs[name]['env']['GODEBUG'], 'asynctimerchan=0')
            self.assertEqual(jobs[name]['services']['postgres']['image'], 'postgres:16')

    def test_archive_guard_order_and_version_dispatch(self):
        jobs = load('company-p0.yml')['jobs']
        steps = jobs['backend']['steps']
        guard = next(i for i,s in enumerate(steps) if s.get('name')=='Exact immutable archive boundary')
        build = next(i for i,s in enumerate(steps) if s.get('name')=='Build')
        self.assertLess(guard,build)
        self.assertEqual(steps[build]['run'],'go build ./...')
        self.assertTrue(any(s.get('run')=='go vet ./...' for s in steps))
        race = next(s for s in jobs['backend-race']['steps'] if s.get('name') == 'Race tests with PostgreSQL')
        self.assertIn('python3 -B tools/ci/backend_single.py', race['run'])
        single_runner = (ROOT / 'tools/ci/backend_single.py').read_text()
        self.assertIn("['go', 'test', '-json', '-race', '-count=1', '-timeout=180s', './...']", single_runner)
        self.assertNotIn("'-run'", single_runner)
        self.assertNotIn('matrix', single_runner)
        self.assertIn('"fsync=on", "full_page_writes=on", "synchronous_commit=on"', race['run'])
        self.assertTrue(any('run_r5_source_version_tests.py' in s.get('run','') for s in steps))
        frontend = jobs['frontend']['steps']
        npm = next(i for i,s in enumerate(frontend) if s.get('run')=='npm ci')
        runner = next(i for i,s in enumerate(frontend) if 'run_r5_source_version_tests.py' in s.get('run',''))
        self.assertLess(npm,runner)
        # The full source suite must still use its isolated version dispatcher.
        # Only the separate single-invocation control suite may use discovery here.
        discoveries = [line.strip() for group in [steps, frontend] for step in group
                       for line in step.get('run', '').splitlines() if 'unittest discover' in line]
        self.assertEqual(discoveries, [])
        for group in ('backend-race',):
            discoveries = [line.strip() for step in jobs[group]['steps']
                           for line in step.get('run', '').splitlines() if 'unittest discover' in line]
            self.assertEqual(discoveries, ["python3 -B -m unittest discover -s tools/ci -p 'test_*.py' -v"])


if __name__ == '__main__':
    unittest.main()
