#!/usr/bin/env python3
"""Publish current source trees through a separate, locally audited Git history.

Python 3.10+ and Git only. This helper never pushes, tags, changes source Git
metadata, deletes a directory, or imports source commits into the public repo.
"""

import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import uuid
from urllib.parse import urlsplit


MANIFEST = "quenching-public.json"
AUDIT = "quenching-public-audit"


class Refusal(Exception):
    """A release precondition was not met; do not guess how to recover."""


def environment():
    result = os.environ.copy()
    # A caller's alternate index/worktree/object store must not redirect our Git
    # commands into the private repository. Keep credential and SSH settings.
    for key in ("GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE",
                "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES",
                "GIT_NAMESPACE", "GIT_SHALLOW_FILE"):
        result.pop(key, None)
    result.update(GIT_OPTIONAL_LOCKS="0", GIT_TERMINAL_PROMPT="0")
    return result


def git_arguments(directory, *arguments):
    return ["git", "-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false",
            "-C", str(directory), *arguments]


def git(directory, *arguments, input_file=None, check=True):
    result = subprocess.run(git_arguments(directory, *arguments), env=environment(),
                            stdin=input_file, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if check and result.returncode:
        detail = result.stderr.decode("utf-8", errors="replace").strip()
        raise Refusal(f"Git {arguments[0]} failed: {detail}")
    return result


def value(directory, *arguments):
    return git(directory, *arguments).stdout.decode("utf-8").strip()


def source_snapshot(source):
    source = source.resolve(strict=True)
    actual = Path(value(source, "rev-parse", "--show-toplevel")).resolve()
    if actual != source:
        raise Refusal("--source must name the source repository root.")
    if value(source, "symbolic-ref", "--short", "HEAD") != "main":
        raise Refusal("Source must be on main.")
    if git(source, "status", "--porcelain", "--untracked-files=no").stdout:
        raise Refusal("Commit or restore tracked source changes before exporting HEAD.")
    return source, value(source, "rev-parse", "HEAD"), value(source, "rev-parse", "HEAD^{tree}")


def staging_path(source, requested):
    output = source / "output"
    if output.resolve() != output:
        raise Refusal("Source output directory must not be a symlink or junction.")
    requested = requested or output / "github-public"
    if not requested.is_absolute():
        requested = source / requested
    stage = requested.resolve()
    if not stage.is_relative_to(output) or stage == output:
        raise Refusal("Staging must be a dedicated directory below source/output/.")
    if requested.absolute() != stage:
        raise Refusal("Staging paths must not contain symlinks, junctions, or '..'.")
    relative = stage.relative_to(source).as_posix() + "/"
    if git(source, "check-ignore", "-q", "--no-index", "--", relative, check=False).returncode:
        raise Refusal("Staging must be ignored by the source repository's .gitignore.")
    return stage


def validate_remote(remote):
    if not remote or remote.startswith("-") or any(c in remote for c in "\r\n\0"):
        raise Refusal("Invalid public remote address.")
    if "::" in remote:
        raise Refusal("Git remote helper commands are not supported.")
    if "://" in remote:
        parsed = urlsplit(remote)
        if parsed.scheme not in {"https", "ssh", "file"}:
            raise Refusal("Use HTTPS, SSH, or a local test repository.")
        if parsed.password or parsed.query or parsed.fragment or (
                parsed.scheme == "https" and parsed.username):
            raise Refusal("Do not embed credentials or query parameters in the public remote URL.")


def remote_refs(directory, remote):
    validate_remote(remote)
    lines = value(directory, "ls-remote", "--refs", "--", remote).splitlines()
    return {line.split("\t", 1)[1]: line.split("\t", 1)[0] for line in lines}


def checked_entries(source, commit):
    entries = []
    seen = set()
    raw = git(source, "ls-tree", "-r", "-z", "--full-tree", commit).stdout
    for item in raw.split(b"\0"):
        if not item:
            continue
        metadata, path = item.split(b"\t", 1)
        mode, kind, oid = metadata.split()
        name = path.decode("utf-8")
        parts = name.split("/")
        lower = name.casefold()
        devices = {"con", "prn", "aux", "nul"} | {
            f"{prefix}{number}" for prefix in ("com", "lpt") for number in range(1, 10)}
        if (lower in seen or any(part in {"", ".", ".."} for part in parts)
                or any(c in name for c in "\\:\0\r\n")
                or any(part.casefold() == ".git" or part.endswith((".", " "))
                       or part.split(".", 1)[0].casefold() in devices for part in parts)):
            raise Refusal(f"Unsafe or nonportable tracked path: {name!r}")
        seen.add(lower)
        private_prefixes = ("docs/internal/", "docs/superpowers/", ".superpowers/",
                            "output/", "logs/", ".codex/", ".agents/", ".aws/")
        if lower.startswith(private_prefixes) or "node_modules" in [p.casefold() for p in parts]:
            raise Refusal(f"Tracked local/internal material must be removed from HEAD first: {name}")
        if kind != b"blob" or mode not in {b"100644", b"100755", b"120000"}:
            raise Refusal(f"Unsupported tracked object (including submodules): {name}")
        entries.append((mode, oid, path))
    return entries


def quoted_path(path):
    # fast-import uses Git's C quoting, which supports octal bytes, not JSON \u escapes.
    return b'"' + b"".join(bytes([c]) if 32 <= c < 127 and c not in (34, 92)
                            else f"\\{c:03o}".encode("ascii") for c in path) + b'"'


def write_import(stream, source, commit, parent, name, email, message,
                 reference="refs/heads/main"):
    entries = checked_entries(source, commit)
    if any(c in name + email for c in "<>\r\n\0") or not name.strip() or not email.strip():
        raise Refusal("Author name/email must be nonempty single-line Git identities.")
    identity = f"{name} <{email}> {int(time.time())} +0000".encode("utf-8")
    message_bytes = message.encode("utf-8")
    stream.write(f"commit {reference}\ncommitter ".encode("ascii") + identity + b"\n")
    stream.write(f"data {len(message_bytes)}\n".encode("ascii") + message_bytes + b"\n")
    if parent:
        stream.write(f"from {parent}\n".encode("ascii"))
    stream.write(b"deleteall\n")
    # Read only reachable current blobs. Never use fast-export, alternates, clone,
    # source remotes, or source .git, all of which can bring private history along.
    with subprocess.Popen(git_arguments(source, "cat-file", "--batch"), env=environment(),
                          stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                          stderr=subprocess.PIPE) as process:
        for mode, oid, path in entries:
            process.stdin.write(oid + b"\n")
            process.stdin.flush()
            header = process.stdout.readline().split()
            if len(header) != 3 or header[1] != b"blob":
                raise Refusal("Could not read a committed source blob.")
            size = int(header[2])
            data = process.stdout.read(size)
            if len(data) != size or process.stdout.read(1) != b"\n":
                raise Refusal("Incomplete committed source blob.")
            if data.startswith(b"version https://git-lfs.github.com/spec/v1"):
                raise Refusal(f"Unresolved Git LFS pointer: {path.decode('utf-8')}")
            stream.write(b"M " + mode + b" inline " + quoted_path(path) + b"\n")
            stream.write(f"data {size}\n".encode("ascii") + data + b"\n")
        process.stdin.close()
        error = process.stderr.read()
        if process.wait():
            raise Refusal("Source blob read failed: " + error.decode("utf-8", errors="replace"))
    stream.write(b"\ndone\n")
    stream.seek(0)


def require_same_source(source, commit):
    if source_snapshot(source)[1] != commit:
        raise Refusal("Source HEAD changed during export; retry from a stable committed main.")


def load_public(source, stage):
    metadata = stage / ".git"
    if not metadata.is_dir() or metadata.resolve() != metadata:
        raise Refusal("Staging must have its own .git directory, not a worktree or symlink.")
    if Path(value(stage, "rev-parse", "--show-toplevel")).resolve() != stage:
        raise Refusal("Staging is not a separate repository.")
    try:
        state = json.loads((metadata / MANIFEST).read_text(encoding="utf-8"))
        required = {"schema", "source_root", "source_commit", "source_tree", "public_commit",
                    "public_tree", "remote", "sequence"}
        if not required.issubset(state) or state["schema"] != 1:
            raise ValueError("unsupported manifest")
    except (OSError, ValueError) as error:
        raise Refusal(f"Missing or invalid local publication manifest: {error}") from error
    if Path(state["source_root"]) != source:
        raise Refusal("Staging was prepared from a different source repository.")
    if (value(stage, "symbolic-ref", "--short", "HEAD") != "main"
            or value(stage, "for-each-ref", "--format=%(refname)", "refs/heads") != "refs/heads/main"
            or value(stage, "rev-parse", "HEAD") != state["public_commit"]
            or value(stage, "rev-parse", "HEAD^{tree}") != state["public_tree"]):
        raise Refusal("Unrecognized staging branch or commit; preserve it and review the local changes.")
    if git(stage, "status", "--porcelain", "--ignored").stdout:
        raise Refusal("Staging has local changes or ignored files; preserve or remove them before exporting.")
    # remote get-url expands user-configured insteadOf rules. Audit the literal
    # configured address while letting Git apply those rules to actual transport.
    if value(stage, "config", "--get", "remote.origin.url") != state["remote"]:
        raise Refusal("Staging origin differs from the recorded public remote.")
    return state


def save_audit(source, commit, tree, stage, remote, sequence, action,
               published_base=None, reviewed_public_commit=None, retained_candidate=None):
    state = {"schema": 1, "source_root": str(source), "source_commit": commit,
             "source_tree": tree, "public_commit": value(stage, "rev-parse", "HEAD"),
             "public_tree": value(stage, "rev-parse", "HEAD^{tree}"), "remote": remote,
             "sequence": sequence, "action": action, "published_base": published_base,
             "reviewed_public_commit": reviewed_public_commit, "retained_candidate": retained_candidate}
    if state["public_tree"] != tree:
        raise Refusal("Exported tree differs from source HEAD; do not publish this candidate.")
    serialized = json.dumps(state, ensure_ascii=False, indent=2) + "\n"
    audit_dir = stage / ".git" / AUDIT
    audit_dir.mkdir(exist_ok=True)
    with (audit_dir / f"{sequence:06d}.json").open("x", encoding="utf-8", newline="\n") as entry:
        entry.write(serialized)
    temporary = stage / ".git" / (MANIFEST + ".tmp")
    temporary.write_text(serialized, encoding="utf-8", newline="\n")
    temporary.replace(stage / ".git" / MANIFEST)
    return state


def source_continues(source, commit, state):
    if git(source, "merge-base", "--is-ancestor", state["source_commit"], commit,
           check=False).returncode:
        raise Refusal("Source main no longer descends from the last audited source commit.")


def remote_status(refs, state):
    remote_main = refs.get("refs/heads/main")
    if remote_main == state["public_commit"]:
        return "up-to-date"
    if not refs and state["sequence"] == 1:
        return "awaiting-first-push"
    return "drift"


def adoption_base(stage, state, incoming, unpublished):
    """Accept a public successor, including a race with one audited unpublished candidate."""
    candidate = state["public_commit"]
    if not git(stage, "merge-base", "--is-ancestor", candidate, incoming, check=False).returncode:
        if unpublished is not None:
            raise Refusal("The declared unpublished candidate already appears in public history.")
        return None
    # An export checks remote P before creating candidate E(parent P). If R lands
    # after that check, a rejected push leaves E and R as siblings. Only that
    # audited, single-parent candidate can be replaced locally; unrelated history
    # and arbitrary local commits remain refusals.
    if unpublished != candidate:
        raise Refusal("Cannot adopt unrelated or rewritten public history. For a verified failed push, "
                      "attest that the exact local candidate was never published with --unpublished-candidate.")
    base = state.get("published_base")
    try:
        audit_dir = stage / ".git" / AUDIT
        recorded = json.loads((audit_dir / f"{state['sequence']:06d}.json").read_text(encoding="utf-8"))
        if recorded != state or not base:
            raise ValueError("candidate has no audited published base")
        if state["action"] == "exported":
            previous = json.loads((audit_dir / f"{state['sequence'] - 1:06d}.json").read_text(encoding="utf-8"))
            if previous["public_commit"] != base:
                raise ValueError("published base differs from previous audit")
        elif state["action"] != "reconciled" or state.get("reviewed_public_commit") != base:
            raise ValueError("not an audited unpublished candidate")
    except (OSError, ValueError, KeyError) as error:
        raise Refusal(f"Cannot adopt unrelated or rewritten public history: {error}") from error
    if (value(stage, "rev-list", "--parents", "-n", "1", candidate).split() != [candidate, base]
            or git(stage, "merge-base", "--is-ancestor", base, incoming, check=False).returncode):
        raise Refusal("Cannot adopt unrelated or rewritten public history.")
    return candidate


def retain_candidate(stage, candidate):
    reference = "refs/quenching-unpublished/" + candidate
    existing = git(stage, "show-ref", "--verify", "--hash", reference, check=False)
    if existing.returncode == 0:
        if existing.stdout.decode("ascii").strip() != candidate:
            raise Refusal("Unpublished-candidate retention ref already points elsewhere.")
    else:
        git(stage, "update-ref", reference, candidate, "0" * len(candidate))


def adopt(source, commit, tree, stage, state, refs, args):
    incoming = refs.get("refs/heads/main")
    if not incoming:
        raise Refusal("Cannot adopt: origin/main is missing.")
    reviewed = args.reviewed_public_commit
    if reviewed is not None and reviewed != incoming:
        raise Refusal("--reviewed-public-commit must equal the exact current origin/main SHA.")
    if args.unpublished_candidate is not None and args.unpublished_candidate != state["public_commit"]:
        raise Refusal("--unpublished-candidate must equal the exact current local candidate SHA.")
    git(stage, "fetch", "--no-tags", "origin", "refs/heads/main")
    if value(stage, "rev-parse", "FETCH_HEAD") != incoming:
        raise Refusal("origin/main changed during adoption; review its new SHA and retry.")
    retained = adoption_base(stage, state, incoming, args.unpublished_candidate)
    same_tree = value(stage, "rev-parse", f"{incoming}^{{tree}}") == tree
    if not same_tree and reviewed is None:
        raise Refusal("Import and review the public changes in source main first; the entire trees must match. "
                      "With additional private work, attest the reviewed/imported exact SHA using "
                      "--reviewed-public-commit, then review the resulting diff before pushing.")
    temporary_ref = None
    try:
        if same_tree:
            checked_entries(source, commit)
            target = incoming
        else:
            temporary_ref = "refs/quenching-candidates/" + uuid.uuid4().hex
            with tempfile.TemporaryFile() as stream:
                write_import(stream, source, commit, incoming, value(stage, "config", "user.name"),
                             value(stage, "config", "user.email"), args.message, temporary_ref)
                require_same_source(source, commit)
                git(stage, "fast-import", "--quiet", input_file=stream)
            target = value(stage, "rev-parse", temporary_ref)
        require_same_source(source, commit)
        if load_public(source, stage) != state:
            raise Refusal("Local publication audit changed during adoption; retry.")
        if remote_refs(stage, state["remote"]).get("refs/heads/main") != incoming:
            raise Refusal("origin/main changed during adoption; review its new SHA and retry.")
        if retained:
            retain_candidate(stage, retained)
        # The remote history remains unchanged. A displaced local candidate is
        # retained above; CAS refuses a concurrent change to local main.
        git(stage, "update-ref", "refs/heads/main", target, state["public_commit"])
        git(stage, "read-tree", "--reset", "-u", "HEAD")
        return save_audit(source, commit, tree, stage, state["remote"], state["sequence"] + 1,
                          "adopted" if same_tree else "reconciled", published_base=incoming,
                          reviewed_public_commit=reviewed, retained_candidate=retained)
    finally:
        if temporary_ref:
            temporary = git(stage, "rev-parse", "--verify", temporary_ref, check=False)
            if temporary.returncode == 0:
                git(stage, "update-ref", "-d", temporary_ref, temporary.stdout.decode("ascii").strip())


def execute(args):
    source, commit, tree = source_snapshot(args.source)
    stage = staging_path(source, args.staging)
    if args.operation == "prepare":
        if stage.exists():
            raise Refusal("Staging already exists; use status/export, or choose another unused output directory.")
        validate_remote(args.remote)
        if not args.offline and remote_refs(source, args.remote):
            raise Refusal("Initial public remote must have no refs; existing repositories are never overwritten.")
        # Finish validation before creating any staging directory.
        with tempfile.TemporaryFile() as stream:
            write_import(stream, source, commit, None, args.author_name, args.author_email, args.message)
            require_same_source(source, commit)
            stage.mkdir(parents=True)
            object_format = value(source, "rev-parse", "--show-object-format")
            git(stage, "init", "--initial-branch=main", "--template=", f"--object-format={object_format}")
            git(stage, "config", "user.name", args.author_name)
            git(stage, "config", "user.email", args.author_email)
            git(stage, "config", "core.autocrlf", "false")
            if os.name == "nt":
                git(stage, "config", "core.symlinks", "false")
            git(stage, "remote", "add", "origin", args.remote)
            git(stage, "fast-import", "--quiet", input_file=stream)
            git(stage, "read-tree", "--reset", "-u", "HEAD")
        result = save_audit(source, commit, tree, stage, args.remote, 1, "prepared")
        result["remote_status"] = "not-checked" if args.offline else "awaiting-first-push"
        return result, 0

    state = load_public(source, stage)
    source_continues(source, commit, state)
    refs = remote_refs(stage, state["remote"])
    condition = remote_status(refs, state)
    if args.operation == "status":
        return {**state, "current_source_commit": commit, "current_source_tree": tree,
                "remote_main": refs.get("refs/heads/main"), "remote_status": condition,
                "source_has_changes": commit != state["source_commit"]}, int(condition == "drift")

    if args.operation == "adopt":
        return adopt(source, commit, tree, stage, state, refs, args), 0

    if condition != "up-to-date":
        raise Refusal("origin/main differs from the last exported public commit. Publish the first candidate, "
                      "or review/import incoming changes into source main and use adopt; never force-push.")
    if tree == state["public_tree"]:
        require_same_source(source, commit)
        if commit == state["source_commit"]:
            return {**state, "action": "unchanged"}, 0
        return save_audit(source, commit, tree, stage, state["remote"], state["sequence"] + 1,
                          "unchanged", published_base=state["public_commit"]), 0
    with tempfile.TemporaryFile() as stream:
        write_import(stream, source, commit, state["public_commit"], value(stage, "config", "user.name"),
                     value(stage, "config", "user.email"), args.message)
        require_same_source(source, commit)
        git(stage, "fast-import", "--quiet", input_file=stream)
        git(stage, "read-tree", "--reset", "-u", "HEAD")
    return save_audit(source, commit, tree, stage, state["remote"], state["sequence"] + 1,
                      "exported", published_base=state["public_commit"]), 0


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="operation", required=True)
    for name in ("prepare", "export", "status", "adopt"):
        command = commands.add_parser(name)
        command.add_argument("--source", type=Path, default=Path.cwd())
        command.add_argument("--staging", type=Path, help="Unused/managed directory under source/output/")
        if name in {"prepare", "export", "adopt"}:
            command.add_argument("--message", default="Publish current source snapshot")
        if name == "adopt":
            command.add_argument("--reviewed-public-commit",
                                 help="Attest that this exact current remote SHA was reviewed and imported "
                                      "into source main; review the resulting candidate diff before pushing")
            command.add_argument("--unpublished-candidate",
                                 help="Attest this exact local candidate was never published, to recover "
                                      "from a rejected push racing with an incoming public update")
        if name == "prepare":
            command.add_argument("--remote", required=True)
            command.add_argument("--author-name", required=True)
            command.add_argument("--author-email", required=True)
            command.add_argument("--offline", action="store_true",
                                 help="Prepare a local candidate without checking the remote")
    try:
        result, code = execute(parser.parse_args())
        print(json.dumps(result, ensure_ascii=False, indent=2))
        return code
    except (Refusal, OSError, UnicodeError) as error:
        print(f"Refused: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
