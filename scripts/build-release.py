#!/usr/bin/env python3
"""从干净且精确固定的源码生成可复用的 CLI 制品。"""
import datetime
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tarfile
import tempfile
import zipfile

TARGETS = [("darwin", "amd64"), ("darwin", "arm64"), ("linux", "amd64"), ("linux", "arm64"), ("windows", "amd64")]


def run(*args, **kwargs):
    return subprocess.check_output(args, text=True, stderr=subprocess.DEVNULL, **kwargs).strip()


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def fail():
    # 不回显参数、路径或底层工具错误，避免诊断泄露凭据。
    print("CLI_RELEASE_BUILD_FAILED", file=sys.stderr)
    sys.exit(1)


def main():
    if len(sys.argv) != 4:
        fail()
    version, commit, destination = sys.argv[1:]
    if not re.fullmatch(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", version) or not re.fullmatch(r"[0-9a-f]{40}", commit):
        fail()
    root = Path.cwd().resolve()
    output = Path(destination).resolve()
    if output == root or root in output.parents:
        fail()
    if run("git", "rev-parse", "HEAD") != commit or run("git", "status", "--porcelain", "--untracked-files=no"):
        fail()
    # 不允许未跟踪或被忽略的文件参与构建；由 git archive 取得准确源码。
    epoch = int(run("git", "show", "-s", "--format=%ct", commit))
    built_at = datetime.datetime.fromtimestamp(epoch, datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    go_version = run("go", "env", "GOVERSION")
    if go_version != "go1.27.2":
        fail()
    expected_names = [f"antapi_{version}_{system}_{arch}." + ("zip" if system == "windows" else "tar.gz") for system, arch in TARGETS]
    base = {"schema_version": 1, "version": version, "commit": commit, "built_at": built_at, "go_version": go_version}
    if output.exists() and any(output.iterdir()):
        manifest = json.loads((output / "manifest.json").read_text())
        if {key: manifest.get(key) for key in base} != base or set(p.name for p in output.iterdir()) != set(expected_names + ["SHA256SUMS", "manifest.json"]):
            fail()
        expected_artifacts = []
        for (system, arch), name in zip(TARGETS, expected_names):
            expected_artifacts.append({"name": name, "sha256": digest(output / name), "os": system, "arch": arch})
        if manifest.get("artifacts") != expected_artifacts:
            fail()
        checksums = "".join(f"{item['sha256']}  {item['name']}\n" for item in expected_artifacts)
        if (output / "SHA256SUMS").read_text() != checksums:
            fail()
        return
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="antapi-release-") as temporary:
        workspace = Path(temporary)
        source = workspace / "source"
        source.mkdir()
        archive_bytes = subprocess.check_output(["git", "archive", "--format=tar", commit], stderr=subprocess.DEVNULL)
        with tarfile.open(fileobj=io.BytesIO(archive_bytes)) as archive:
            for member in archive.getmembers():
                path = Path(member.name)
                if path.is_absolute() or ".." in path.parts or not (member.isfile() or member.isdir()):
                    fail()
            archive.extractall(source)
        # 直接从固定提交的源码构建；不读工作树中的未跟踪 Go 文件。
        artifacts = workspace / "artifacts"
        artifacts.mkdir()
        entries = []
        for (system, arch), name in zip(TARGETS, expected_names):
            member = "antapi.exe" if system == "windows" else "antapi"
            binary = workspace / member
            environment = dict(os.environ, CGO_ENABLED="0", GOOS=system, GOARCH=arch, GOTOOLCHAIN="local", GOWORK="off", GOFLAGS="", GOEXPERIMENT="", GOAMD64="v1", GOARM64="v8.0")
            flags = f"-s -w -buildid= -X main.version={version} -X main.commit={commit} -X main.builtAt={built_at}"
            subprocess.run(["go", "build", "-trimpath", "-buildvcs=false", "-ldflags", flags, "-o", str(binary), "./cmd/antapi"], cwd=source, env=environment, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            target = artifacts / name
            if system == "windows":
                stamp = datetime.datetime.fromtimestamp(max(epoch, 315532800), datetime.timezone.utc)
                info = zipfile.ZipInfo(member, stamp.timetuple()[:6])
                info.create_system = 3
                info.compress_type = zipfile.ZIP_DEFLATED
                info.external_attr = 0o755 << 16
                with zipfile.ZipFile(target, "w") as archive:
                    archive.writestr(info, binary.read_bytes())
            else:
                with target.open("wb") as raw:
                    with gzip.GzipFile(fileobj=raw, mode="wb", filename="", mtime=epoch) as compressed:
                        with tarfile.open(fileobj=compressed, mode="w", format=tarfile.USTAR_FORMAT) as archive:
                            info = tarfile.TarInfo(member)
                            info.size = binary.stat().st_size
                            info.mode = 0o755
                            info.mtime = epoch
                            archive.addfile(info, io.BytesIO(binary.read_bytes()))
            entries.append({"name": name, "sha256": digest(target), "os": system, "arch": arch})
        (artifacts / "SHA256SUMS").write_text("".join(f"{item['sha256']}  {item['name']}\n" for item in entries))
        (artifacts / "manifest.json").write_text(json.dumps(dict(base, artifacts=entries), indent=2, sort_keys=True) + "\n")
        # 所有目标成功后才暴露目录；已有非空目录绝不覆盖。
        if output.exists():
            output.rmdir()
        os.replace(artifacts, output) if workspace.stat().st_dev == output.parent.stat().st_dev else copy_artifacts(artifacts, output)


def copy_artifacts(source, output):
    import shutil
    with tempfile.TemporaryDirectory(prefix=".antapi-candidate-", dir=output.parent) as temporary:
        candidate = Path(temporary) / "artifacts"
        shutil.copytree(source, candidate)
        os.replace(candidate, output)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError, tarfile.TarError):
        fail()
