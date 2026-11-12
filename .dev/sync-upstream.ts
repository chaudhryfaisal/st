import { Command } from "@jpillora/take";

// run executes git and returns trimmed stdout; a non-zero exit throws unless
// `allowFail` is set, in which case ok=false is returned.
function git(args: string[], allowFail = false) {
  const p = Bun.spawnSync(["git", ...args], { stdout: "pipe", stderr: "pipe" });
  const out = p.stdout.toString().trim();
  if (p.exitCode !== 0 && !allowFail) {
    throw new Error(`git ${args.join(" ")} failed:\n${p.stderr.toString().trim() || out}`);
  }
  return { ok: p.exitCode === 0, out, err: p.stderr.toString().trim() };
}

function gitLive(args: string[]) {
  const p = Bun.spawnSync(["git", ...args], { stdout: "inherit", stderr: "inherit" });
  return p.exitCode === 0;
}

export default Command({
  name: "sync-upstream",
  description:
    "Fetch jpillora/cloud-torrent and merge (or rebase onto) its main branch",
  flags: {
    check: {
      initial: false,
      description: "Only fetch and report what is incoming; change nothing",
    },
    rebase: {
      initial: false,
      description: "Rebase onto upstream instead of merging (needs a force push)",
    },
    verify: {
      initial: false,
      description: "After syncing, run check-types and the Go tests",
    },
    push: {
      initial: false,
      description: "After syncing, push the branch to origin",
    },
    remote: {
      initial: "upstream",
      description: "Name of the upstream git remote",
    },
    remoteUrl: {
      initial: "https://github.com/jpillora/cloud-torrent.git",
      description: "URL used to create the upstream remote when it is missing",
    },
    branch: {
      initial: "main",
      description: "Branch to sync on both sides",
    },
  },
  async run(ctx) {
    try {
      await sync(ctx);
    } catch (error) {
      console.error(`sync-upstream: ${(error as Error).message}`);
      process.exit(1);
    }
  },
});

async function sync({ cmd, flags }: { cmd: (name: string) => Promise<unknown>; flags: Record<string, any> }) {
  {
    const { remote, branch } = flags;
    const current = git(["rev-parse", "--abbrev-ref", "HEAD"]).out;
    if (current !== branch) {
      throw new Error(`on branch ${current}; check out ${branch} first`);
    }
    if (!flags.check && git(["status", "--porcelain", "--untracked-files=no"]).out) {
      throw new Error("working tree has uncommitted changes; commit or stash them first");
    }

    if (!git(["remote", "get-url", remote], true).ok) {
      console.log(`adding remote ${remote} -> ${flags.remoteUrl}`);
      git(["remote", "add", remote, flags.remoteUrl]);
    }
    console.log(`fetching ${remote}/${branch}...`);
    git(["fetch", remote, branch]);

    const upstream = `${remote}/${branch}`;
    const [ahead, behind] = git(["rev-list", "--left-right", "--count", `HEAD...${upstream}`])
      .out.split(/\s+/).map(Number);
    console.log(`${branch} is ${ahead} commit(s) ahead of and ${behind} behind ${upstream}`);
    if (behind === 0) {
      console.log("already up to date with upstream");
      return;
    }
    console.log(`\nincoming from ${upstream}:`);
    console.log(git(["log", "--oneline", "--no-decorate", "-n", "40", `HEAD..${upstream}`]).out);
    if (behind > 40) console.log(`... and ${behind - 40} more`);
    if (flags.check) return;

    const verb = flags.rebase ? "rebase" : "merge";
    const ok = flags.rebase
      ? gitLive(["rebase", upstream])
      : gitLive(["merge", "--no-edit", upstream]);
    if (!ok) {
      const conflicts = git(["diff", "--name-only", "--diff-filter=U"], true).out;
      console.error(`\n${verb} stopped with conflicts in:\n${conflicts || "(none listed)"}`);
      console.error(
        `resolve them and ${flags.rebase ? "git rebase --continue" : "git commit"}, ` +
          `or undo with git ${verb} --abort`,
      );
      process.exit(1);
    }
    console.log(`\n${verb} complete`);

    if (flags.verify) {
      await cmd("check-types");
      await cmd("test");
    }
    if (flags.push) {
      const args = flags.rebase ? ["push", "--force-with-lease", "origin", branch] : ["push", "origin", branch];
      if (!gitLive(args)) process.exit(1);
    } else {
      console.log(`review, then: git push${flags.rebase ? " --force-with-lease" : ""} origin ${branch}`);
    }
  }
}
