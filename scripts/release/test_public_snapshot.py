"""Offline integration tests: all remotes are disposable local bare repositories."""

import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("public_snapshot.py")


class PublicSnapshotTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="quenching-public-test-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name).resolve()
        self.source = self.root / "private"
        self.remote = self.root / "public.git"
        self.stage = self.source / "output" / "github-public"
        self.environment = {key: value for key, value in os.environ.items()
                            if not key.startswith("GIT_")}
        self.environment.update(GIT_CONFIG_NOSYSTEM="1", GIT_CONFIG_GLOBAL=os.devnull,
                                GIT_TERMINAL_PROMPT="0", GIT_CONFIG_COUNT="1",
                                GIT_CONFIG_KEY_0="safe.directory", GIT_CONFIG_VALUE_0="*",
                                PYTHONUTF8="1", PYTHONIOENCODING="utf-8")
        self.git(self.root, "init", "--bare", "--initial-branch=main", str(self.remote))
        self.git(self.root, "init", "--initial-branch=main", str(self.source))
        self.identity(self.source)
        self.git(self.source, "config", "core.symlinks", "false")
        self.git(self.source, "remote", "add", "origin", "https://private.example/old.git")
        self.write(".gitignore", "output/\nnode_modules/\nlogs/\ndocs/internal/\n")
        self.write("docs/internal/old.md", "old private history must never be published\n")
        self.git(self.source, "add", "--force", ".gitignore", "docs/internal/old.md")
        self.git(self.source, "commit", "-m", "Private historical material")
        self.git(self.source, "rm", "docs/internal/old.md")
        self.write("README.md", "Current public source\n")
        self.write("run.sh", "#!/bin/sh\necho ready\n")
        self.git(self.source, "add", ".")
        self.git(self.source, "update-index", "--chmod=+x", "run.sh")
        self.git(self.source, "commit", "-m", "Clean current source")

    def run_process(self, arguments, cwd=None, expected=0):
        result = subprocess.run(arguments, cwd=cwd, env=self.environment,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                text=True, encoding="utf-8")
        if expected is not None:
            self.assertEqual(expected, result.returncode, result.stdout + result.stderr)
        return result

    def git(self, directory, *arguments, expected=0):
        return self.run_process(["git", "-C", str(directory), *arguments],
                                expected=expected).stdout.strip()

    def identity(self, directory):
        self.git(directory, "config", "user.name", "Public Test")
        self.git(directory, "config", "user.email", "public@example.invalid")

    def write(self, name, content):
        path = self.source / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding="utf-8", newline="\n")

    def source_git_files(self):
        return {str(path.relative_to(self.source / ".git")): path.read_bytes()
                for path in (self.source / ".git").rglob("*") if path.is_file()}

    def command(self, operation, *arguments, expected=0):
        return self.run_process([sys.executable, str(SCRIPT), operation,
                                 "--source", str(self.source), *arguments], expected=expected)

    def prepare(self, expected=0):
        return self.command("prepare", "--remote", str(self.remote),
                            "--author-name", "Public Test", "--author-email",
                            "public@example.invalid", expected=expected)

    def push(self):
        self.git(self.stage, "push", "origin", "main:main")

    def advance_source(self, name="README.md", content="Next public source\n"):
        self.write(name, content)
        self.git(self.source, "add", "--", name)
        self.git(self.source, "commit", "-m", "Private development change")

    def test_prepare_exports_only_current_tree_with_no_private_history_or_git_changes(self):
        # Copying the working directory or source .git instead of current blobs breaks this.
        self.write("scratch.txt", "untracked private note\n")
        self.write("docs/internal/local.md", "ignored private note\n")
        self.write("node_modules/dep/index.js", "ignored dependency\n")
        before = self.source_git_files()
        result = json.loads(self.prepare().stdout)
        self.assertEqual(before, self.source_git_files())
        self.assertEqual("1", self.git(self.stage, "rev-list", "--count", "HEAD"))
        self.assertEqual("main", self.git(self.stage, "branch", "--format=%(refname:short)"))
        self.assertEqual(self.git(self.source, "rev-parse", "HEAD^{tree}"),
                         self.git(self.stage, "rev-parse", "HEAD^{tree}"))
        self.assertEqual(".gitignore\nREADME.md\nrun.sh",
                         self.git(self.stage, "ls-tree", "-r", "--name-only", "HEAD"))
        self.assertTrue(self.git(self.stage, "ls-tree", "HEAD", "run.sh").startswith("100755"))
        self.assertNotIn("private.example", (self.stage / ".git" / "config").read_text())
        old_source = self.git(self.source, "rev-parse", "HEAD~1")
        self.git(self.stage, "cat-file", "-e", old_source, expected=1)
        self.assertEqual(self.git(self.source, "rev-parse", "HEAD"), result["source_commit"])
        manifest = json.loads((self.stage / ".git" / "quenching-public.json").read_text())
        self.assertEqual(self.git(self.stage, "rev-parse", "HEAD"), manifest["public_commit"])
        self.assertEqual("awaiting-first-push", json.loads(self.command("status").stdout)["remote_status"])

    def test_export_keeps_public_history_and_maps_new_source_without_importing_old_history(self):
        # Resetting the staging repository would lose the first public parent.
        self.prepare()
        self.push()
        first = self.git(self.stage, "rev-parse", "HEAD")
        git_config = (self.stage / ".git" / "config").read_bytes()
        self.advance_source()
        before = self.source_git_files()
        self.command("export")
        self.assertEqual(before, self.source_git_files())
        self.assertEqual(first, self.git(self.stage, "rev-parse", "HEAD^"))
        self.assertEqual("2", self.git(self.stage, "rev-list", "--count", "HEAD"))
        self.assertEqual(self.git(self.source, "rev-parse", "HEAD^{tree}"),
                         self.git(self.stage, "rev-parse", "HEAD^{tree}"))
        self.assertEqual(git_config, (self.stage / ".git" / "config").read_bytes())
        self.push()
        self.assertEqual("up-to-date", json.loads(self.command("status").stdout)["remote_status"])
        self.assertEqual(2, len(list((self.stage / ".git" / "quenching-public-audit").glob("*.json"))))

    def test_incoming_public_commit_refuses_export_without_changing_any_repository(self):
        # Ignoring remote drift would overwrite an incoming contribution on the next push.
        self.prepare()
        self.push()
        contributor = self.root / "contributor"
        self.git(self.root, "clone", str(self.remote), str(contributor))
        self.identity(contributor)
        (contributor / "contribution.md").write_text("A public contribution\n", newline="\n")
        self.git(contributor, "add", ".")
        self.git(contributor, "commit", "-m", "Incoming public contribution")
        self.git(contributor, "push", "origin", "main")
        public_before = self.git(self.stage, "rev-parse", "HEAD")
        manifest_before = (self.stage / ".git" / "quenching-public.json").read_bytes()
        self.advance_source()
        source_before = self.source_git_files()
        result = self.command("export", expected=1)
        self.assertIn("origin/main", result.stderr)
        self.assertEqual(public_before, self.git(self.stage, "rev-parse", "HEAD"))
        self.assertEqual(manifest_before, (self.stage / ".git" / "quenching-public.json").read_bytes())
        self.assertEqual(source_before, self.source_git_files())
        self.command("status", expected=1)

    def test_prepare_refuses_existing_remote_and_does_not_create_staging(self):
        # Accepting any existing remote ref could replace an unrelated repository.
        self.git(self.source, "push", str(self.remote), "main:main")
        self.prepare(expected=1)
        self.assertFalse(self.stage.exists())

    def test_prepare_refuses_existing_directory_and_paths_outside_ignored_output(self):
        # A path mistake must never repurpose an existing directory or source .git.
        self.stage.mkdir(parents=True)
        marker = self.stage / "keep.txt"
        marker.write_text("do not modify")
        self.prepare(expected=1)
        self.assertEqual("do not modify", marker.read_text())
        outside = self.root / "outside"
        self.command("prepare", "--staging", str(outside), "--remote", str(self.remote),
                     "--author-name", "Public Test", "--author-email", "public@example.invalid",
                     expected=1)
        self.assertFalse(outside.exists())

    def test_prepare_refuses_unresolved_lfs_pointer_before_creating_repository(self):
        # Publishing pointer text instead of the real binary silently produces a broken release.
        self.advance_source("asset.bin", "version https://git-lfs.github.com/spec/v1\n"
                            "oid sha256:" + "0" * 64 + "\nsize 10\n")
        result = self.prepare(expected=1)
        self.assertIn("LFS", result.stderr)
        self.assertFalse(self.stage.exists())

    def test_prepare_refuses_tracked_private_records_and_uncommitted_source_changes(self):
        # A committed internal record must not slip through just because .gitignore matches it.
        self.write("docs/internal/secret.md", "private\n")
        self.git(self.source, "add", "--force", "docs/internal/secret.md")
        self.git(self.source, "commit", "-m", "Accidentally tracked internal record")
        self.prepare(expected=1)
        self.assertFalse(self.stage.exists())
        self.git(self.source, "rm", "docs/internal/secret.md")
        self.git(self.source, "commit", "-m", "Remove internal record")
        self.write("README.md", "Uncommitted content\n")
        self.prepare(expected=1)
        self.assertFalse(self.stage.exists())

    def test_export_refuses_unrecognized_local_commit(self):
        # Adopting arbitrary local commits would break the controlled source-to-public mapping.
        self.prepare()
        self.push()
        self.git(self.stage, "commit", "--allow-empty", "-m", "Unexpected local public commit")
        head = self.git(self.stage, "rev-parse", "HEAD")
        self.advance_source()
        self.command("export", expected=1)
        self.assertEqual(head, self.git(self.stage, "rev-parse", "HEAD"))

    def test_offline_prepare_creates_reviewable_candidate_without_contacting_remote(self):
        # Offline mode must work before GitHub exists, but cannot relax export's remote check.
        missing_remote = self.root / "does-not-exist.git"
        result = self.command("prepare", "--offline", "--remote", str(missing_remote),
                              "--author-name", "Public Test", "--author-email",
                              "public@example.invalid")
        self.assertEqual("not-checked", json.loads(result.stdout)["remote_status"])
        self.assertEqual("1", self.git(self.stage, "rev-list", "--count", "HEAD"))
        head = self.git(self.stage, "rev-parse", "HEAD")
        self.advance_source()
        self.command("export", expected=1)
        self.assertEqual(head, self.git(self.stage, "rev-parse", "HEAD"))

    def test_adopt_requires_reviewed_imported_source_tree_then_allows_next_export(self):
        # Adoption without whole-tree verification would conceal an unreviewed incoming change.
        self.prepare()
        self.push()
        contributor = self.root / "contributor"
        self.git(self.root, "clone", str(self.remote), str(contributor))
        self.identity(contributor)
        (contributor / "contribution.md").write_text("Reviewed contribution\n", newline="\n")
        self.git(contributor, "add", ".")
        self.git(contributor, "commit", "-m", "Public contribution")
        self.git(contributor, "push", "origin", "main")
        original_head = self.git(self.stage, "rev-parse", "HEAD")
        original_manifest = (self.stage / ".git" / "quenching-public.json").read_bytes()
        self.command("adopt", expected=1)
        self.assertEqual(original_head, self.git(self.stage, "rev-parse", "HEAD"))
        self.assertEqual(original_manifest, (self.stage / ".git" / "quenching-public.json").read_bytes())
        self.advance_source("contribution.md", "Reviewed contribution\n")
        source_before = self.source_git_files()
        result = self.command("adopt")
        self.assertEqual("adopted", json.loads(result.stdout)["action"])
        self.assertEqual(source_before, self.source_git_files())
        self.assertEqual(self.git(contributor, "rev-parse", "HEAD"),
                         self.git(self.stage, "rev-parse", "HEAD"))
        self.advance_source()
        self.command("export")
        self.push()
        self.assertEqual("up-to-date", json.loads(self.command("status").stdout)["remote_status"])

    def test_same_tree_source_commit_does_not_create_duplicate_public_commit(self):
        # Metadata-only source commits should update the audit without creating empty releases.
        self.prepare()
        self.push()
        head = self.git(self.stage, "rev-parse", "HEAD")
        self.git(self.source, "commit", "--allow-empty", "-m", "Private metadata only")
        result = self.command("export")
        self.assertEqual("unchanged", json.loads(result.stdout)["action"])
        self.assertEqual(head, self.git(self.stage, "rev-parse", "HEAD"))
        manifest = json.loads((self.stage / ".git" / "quenching-public.json").read_text())
        self.assertEqual(self.git(self.source, "rev-parse", "HEAD"), manifest["source_commit"])

    def test_symlink_blob_and_mode_are_preserved_without_following_the_link(self):
        # Reading filesystem contents would dereference links and lose their Git mode.
        target = b"README.md"
        oid = subprocess.run(["git", "-C", str(self.source), "hash-object", "-w", "--stdin"],
                             input=target, env=self.environment, capture_output=True, check=True)
        self.git(self.source, "update-index", "--add", "--cacheinfo",
                 "120000," + oid.stdout.decode().strip() + ",readme-link")
        self.git(self.source, "checkout-index", "--", "readme-link")
        self.git(self.source, "commit", "-m", "Track a symbolic link")
        self.prepare()
        self.assertTrue(self.git(self.stage, "ls-tree", "HEAD", "readme-link").startswith("120000"))
        self.assertEqual("README.md", self.git(self.stage, "show", "HEAD:readme-link"))

    def test_adopt_refuses_unrelated_public_history_even_if_the_current_trees_match(self):
        # Tree equality alone must never authorize replacing the known public history.
        self.prepare()
        self.push()
        original = self.git(self.stage, "rev-parse", "HEAD")
        # A parentless commit models a rewritten remote, without any force-push.
        tree = self.git(self.stage, "rev-parse", "HEAD^{tree}")
        replacement = self.git(self.stage, "commit-tree", tree, "-m", "Unrelated root")
        self.git(self.stage, "push", "origin", replacement + ":refs/heads/unrelated")
        # Mutate only the disposable bare fixture, never an external repository.
        self.git(self.remote, "update-ref", "refs/heads/main", replacement)
        result = self.command("adopt", expected=1)
        self.assertIn("unrelated", result.stderr)
        self.assertEqual(original, self.git(self.stage, "rev-parse", "HEAD"))

    def test_global_url_rewrite_does_not_look_like_a_changed_origin(self):
        # get-url expands insteadOf; comparing that value would reject an unchanged origin.
        alias = "https://public.example/test.git"
        config = self.root / "test-global.gitconfig"
        config.write_text('[url "' + self.remote.as_posix() + '"]\n'
                          '\tinsteadOf = ' + alias + '\n', encoding="utf-8", newline="\n")
        self.environment["GIT_CONFIG_GLOBAL"] = str(config)
        self.command("prepare", "--remote", alias, "--author-name", "Public Test",
                     "--author-email", "public@example.invalid")
        self.push()
        self.assertNotEqual(alias, self.git(self.stage, "remote", "get-url", "origin"))
        self.assertEqual(alias, self.git(self.stage, "config", "--get", "remote.origin.url"))
        self.assertEqual("up-to-date", json.loads(self.command("status").stdout)["remote_status"])
        self.advance_source()
        self.command("export")

    def public_contribution(self):
        contributor = self.root / "contributor"
        self.git(self.root, "clone", str(self.remote), str(contributor))
        self.identity(contributor)
        (contributor / "contribution.md").write_text("Reviewed contribution\n", newline="\n")
        self.git(contributor, "add", ".")
        self.git(contributor, "commit", "-m", "Public contribution")
        self.git(contributor, "push", "origin", "main")
        return self.git(contributor, "rev-parse", "HEAD")

    def test_adopt_recovers_after_push_race_and_preserves_unpublished_candidate(self):
        # The last candidate E can be a sibling of incoming R, both descending from published P.
        self.prepare()
        self.push()
        published = self.git(self.stage, "rev-parse", "HEAD")
        self.advance_source()
        self.command("export")
        candidate = self.git(self.stage, "rev-parse", "HEAD")
        incoming = self.public_contribution()
        self.git(self.stage, "push", "origin", "main:main", expected=1)
        self.write("README.md", "Current public source\n")
        self.write("contribution.md", "Reviewed contribution\n")
        self.git(self.source, "add", ".")
        self.git(self.source, "commit", "-m", "Import reviewed public state")
        before = self.source_git_files()
        self.command("adopt", expected=1)
        self.command("adopt", "--unpublished-candidate", published, expected=1)
        self.command("adopt", "--unpublished-candidate", candidate)
        self.assertEqual(before, self.source_git_files())
        self.assertEqual(incoming, self.git(self.stage, "rev-parse", "HEAD"))
        self.assertEqual(candidate, self.git(self.stage, "rev-parse",
                         "refs/quenching-unpublished/" + candidate))
        self.assertEqual(published, self.git(self.stage, "rev-parse", candidate + "^"))
        self.assertEqual("main", self.git(self.stage, "branch", "--format=%(refname:short)"))
        self.advance_source()
        self.command("export")
        self.push()
        self.assertEqual("", self.git(self.remote, "for-each-ref", "refs/quenching-unpublished"))

    def test_reviewed_adopt_keeps_private_work_and_parents_new_candidate_at_reviewed_remote(self):
        # Whole-tree proof is impossible with private work in flight; an exact-SHA attestation is explicit.
        self.prepare()
        self.push()
        self.advance_source()
        self.command("export")
        unpublished = self.git(self.stage, "rev-parse", "HEAD")
        incoming = self.public_contribution()
        self.advance_source("contribution.md", "Reviewed contribution\n")
        manifest = (self.stage / ".git" / "quenching-public.json").read_bytes()
        self.command("adopt", expected=1)
        self.command("adopt", "--reviewed-public-commit", unpublished, expected=1)
        self.assertEqual(unpublished, self.git(self.stage, "rev-parse", "HEAD"))
        self.assertEqual(manifest, (self.stage / ".git" / "quenching-public.json").read_bytes())
        before = self.source_git_files()
        self.command("adopt", "--reviewed-public-commit", incoming, expected=1)
        result = self.command("adopt", "--reviewed-public-commit", incoming,
                              "--unpublished-candidate", unpublished,
                              "--message", "Integrate reviewed public contribution")
        self.assertEqual("reconciled", json.loads(result.stdout)["action"])
        self.assertEqual(before, self.source_git_files())
        self.assertEqual(incoming, self.git(self.stage, "rev-parse", "HEAD^"))
        self.assertEqual(self.git(self.source, "rev-parse", "HEAD^{tree}"),
                         self.git(self.stage, "rev-parse", "HEAD^{tree}"))
        self.assertEqual(unpublished, self.git(self.stage, "rev-parse",
                         "refs/quenching-unpublished/" + unpublished))
        self.assertIn("Next public source", self.git(self.stage, "show", "HEAD:README.md"))
        self.assertIn("Reviewed contribution", self.git(self.stage, "show", "HEAD:contribution.md"))
        self.push()
        self.assertEqual("up-to-date", json.loads(self.command("status").stdout)["remote_status"])

    def test_reviewed_adopt_still_refuses_unrelated_remote_history(self):
        # An explicit reviewed SHA cannot authorize replacing the audited public lineage.
        self.prepare()
        self.push()
        self.advance_source()
        self.command("export")
        original = self.git(self.stage, "rev-parse", "HEAD")
        tree = self.git(self.stage, "rev-parse", "HEAD^{tree}")
        replacement = self.git(self.stage, "commit-tree", tree, "-m", "Unrelated root")
        self.git(self.stage, "push", "origin", replacement + ":refs/heads/unrelated")
        self.git(self.remote, "update-ref", "refs/heads/main", replacement)
        self.command("adopt", "--reviewed-public-commit", replacement, expected=1)
        self.assertEqual(original, self.git(self.stage, "rev-parse", "HEAD"))

    def test_adopt_does_not_silently_treat_rewritten_published_commit_as_unpublished(self):
        # E may have been pushed before a remote rewrite; a sibling alone is not proof of failed push.
        self.prepare()
        self.push()
        published_parent = self.git(self.stage, "rev-parse", "HEAD")
        self.advance_source()
        self.command("export")
        self.push()
        published = self.git(self.stage, "rev-parse", "HEAD")
        tree = self.git(self.stage, "rev-parse", "HEAD^{tree}")
        replacement = self.git(self.stage, "commit-tree", tree, "-p", published_parent,
                               "-m", "Rewritten published history")
        self.git(self.stage, "push", "origin", replacement + ":refs/heads/rewritten")
        self.git(self.remote, "update-ref", "refs/heads/main", replacement)
        manifest = (self.stage / ".git" / "quenching-public.json").read_bytes()
        self.command("adopt", expected=1)
        self.command("adopt", "--reviewed-public-commit", replacement, expected=1)
        self.assertEqual(published, self.git(self.stage, "rev-parse", "HEAD"))
        self.assertEqual(manifest, (self.stage / ".git" / "quenching-public.json").read_bytes())

    def test_export_preserves_ignored_local_staging_files_that_would_be_overwritten(self):
        # read-tree --reset would otherwise replace ignored local content without warning.
        self.advance_source(".gitignore", "output/\nnode_modules/\nlogs/\ndocs/internal/\ncache/\n")
        self.prepare()
        self.push()
        local = self.stage / "cache" / "item.txt"
        local.parent.mkdir()
        local.write_text("Local data to preserve\n", newline="\n")
        self.write("cache/item.txt", "Newly tracked source file\n")
        self.git(self.source, "add", "--force", "cache/item.txt")
        self.git(self.source, "commit", "-m", "Track previously ignored path")
        head = self.git(self.stage, "rev-parse", "HEAD")
        self.command("export", expected=1)
        self.assertEqual("Local data to preserve\n", local.read_text())
        self.assertEqual(head, self.git(self.stage, "rev-parse", "HEAD"))


if __name__ == "__main__":
    unittest.main()
