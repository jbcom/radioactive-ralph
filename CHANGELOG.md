---
title: CHANGELOG
lastUpdated: 2026-07-26
---

# Changelog

All notable changes to this project will be documented in this file.
Format: [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/), [Semantic Versioning](https://semver.org/).

Older entries preserve the product language that was true when those releases
shipped. That means historical sections may still mention MCP, plugins,
supervisors, or the archived Python implementation even though those are no
longer part of the live contract.

## [0.35.5](https://github.com/jbcom/radioactive-ralph/compare/v0.35.4...v0.35.5) (2026-08-24)


### Bug Fixes

* **release:** authorize sealed draft verifier ([f8f2298](https://github.com/jbcom/radioactive-ralph/commit/f8f22985f19ba06593a6393fb54a9313db8f9e39))
* **release:** authorize sealed draft verifier ([0ecee05](https://github.com/jbcom/radioactive-ralph/commit/0ecee05215b26c4d4831f965d024a15aa7922044))
* **release:** scope draft verifier credential ([0e01c4b](https://github.com/jbcom/radioactive-ralph/commit/0e01c4b20b68d7728a5e8d93f19fbc5d9901e0f8))

## [0.35.4](https://github.com/jbcom/radioactive-ralph/compare/v0.35.3...v0.35.4) (2026-08-24)


### Bug Fixes

* **release:** stream draft asset API responses ([b517cb4](https://github.com/jbcom/radioactive-ralph/commit/b517cb4f45328d8751809c04344da292d5888a40))
* **release:** verify draft assets through API ([f6201e4](https://github.com/jbcom/radioactive-ralph/commit/f6201e471da0fe9f100fdd2e62def21ffb168571))
* **release:** verify draft assets through API ([fe34882](https://github.com/jbcom/radioactive-ralph/commit/fe34882a71c4f065efae5b14103bdecf58ce0a69))

## [0.35.3](https://github.com/jbcom/radioactive-ralph/compare/v0.35.2...v0.35.3) (2026-08-24)


### Bug Fixes

* **release:** inspect draft seal through gh ([2c40ca7](https://github.com/jbcom/radioactive-ralph/commit/2c40ca7047f9250775248c25591a4aba0a8bf0cf))
* **release:** inspect draft seal through gh ([68929cd](https://github.com/jbcom/radioactive-ralph/commit/68929cda8f5e4d8e588bf57e95bd67de8ae07da3))
* **release:** read draft promotion state through gh ([ffc16dd](https://github.com/jbcom/radioactive-ralph/commit/ffc16ddb02d32d61f8aa54110b647dcc8ef527e1))

## [0.35.2](https://github.com/jbcom/radioactive-ralph/compare/v0.35.1...v0.35.2) (2026-08-24)


### Bug Fixes

* **release:** inspect draft releases through gh ([e75c10b](https://github.com/jbcom/radioactive-ralph/commit/e75c10b75e7d6db184cd92eaaa9abff9eadc99c2))
* **release:** inspect draft releases through gh ([99f77a1](https://github.com/jbcom/radioactive-ralph/commit/99f77a19e313c06e4838623084e5478361623392))

## [0.35.1](https://github.com/jbcom/radioactive-ralph/compare/v0.35.0...v0.35.1) (2026-08-24)


### Bug Fixes

* avoid environment allocation overflow ([21bbb4a](https://github.com/jbcom/radioactive-ralph/commit/21bbb4af88dbfd7aa25484c339062fb1b670c8a0))
* avoid environment allocation overflow ([a9d1937](https://github.com/jbcom/radioactive-ralph/commit/a9d193779d8aeb7f3690992c8fc4fc1526be6dcf))
* **provider:** bound process cancellation wait ([84b5729](https://github.com/jbcom/radioactive-ralph/commit/84b5729e48b1ac0d3422bebb746a9814d134b257))
* **release:** target GUI checksum upload ([3045cc1](https://github.com/jbcom/radioactive-ralph/commit/3045cc18e6b8f6300284244beebb6b509055f1b2))

## [0.35.0](https://github.com/jbcom/radioactive-ralph/compare/v0.34.0...v0.35.0) (2026-08-24)


### Features

* **agent:** dispatch Windows provider turns through wsl.exe instead of ConPTY ([39444d1](https://github.com/jbcom/radioactive-ralph/commit/39444d12509539b8083074158775a798d01b32af))
* **doctor:** add wsl dispatch check and internal/wsldistro package ([6d83e20](https://github.com/jbcom/radioactive-ralph/commit/6d83e20f4eb0e2c4c44b459b294b8e4fb1e36043))
* **packaging:** add WSL2 rootfs build tooling for bundled radioactive-ralph distro ([d1885af](https://github.com/jbcom/radioactive-ralph/commit/d1885af65b5b1ba0e7ce8df4a4db4ecc318ac19d))
* **provider:** add GitHub Copilot CLI as a first-class provider ([25a29b9](https://github.com/jbcom/radioactive-ralph/commit/25a29b934ef52aaf8c7b0960ed1fb3e6cc80e8e8))


### Bug Fixes

* address maintainer review — auto-provisioning, dep tidiness, doc fixes ([bc59c01](https://github.com/jbcom/radioactive-ralph/commit/bc59c01336eb9e57e450cf1920c63c38f5713aee))
* **agent:** restore ptyMaster.Stat() and decode WSL distro names as real UTF-16LE ([2daa9b3](https://github.com/jbcom/radioactive-ralph/commit/2daa9b3645a55c8ebc151600dd2bdd04e57b392e))
* **makefile:** add --init to test-linux* Docker targets, fix false test failures ([58d1ccd](https://github.com/jbcom/radioactive-ralph/commit/58d1ccd06d653fe845fef38daece798164f3e233))

## [0.34.0](https://github.com/jbcom/radioactive-ralph/compare/v0.33.1...v0.34.0) (2026-08-20)


### Features

* add canonical cross-adapter enforcement policy ([#375](https://github.com/jbcom/radioactive-ralph/issues/375)) ([cabf63f](https://github.com/jbcom/radioactive-ralph/commit/cabf63f4a7394a3626df30c50e9ac07e32ae9c95))
* add generated completion-enforcement adapters ([#378](https://github.com/jbcom/radioactive-ralph/issues/378)) ([306d618](https://github.com/jbcom/radioactive-ralph/commit/306d618dcf2c35999dad05b72b0ac90bc41ea233))
* **cli:** add --plan to status, so a recent run stays readable ([#340](https://github.com/jbcom/radioactive-ralph/issues/340)) ([b84dbc9](https://github.com/jbcom/radioactive-ralph/commit/b84dbc975992f7d518ba84e3f54477e70e278184))
* **cli:** add `plan delete`, wiring store.DeletePlan to an operator surface ([#339](https://github.com/jbcom/radioactive-ralph/issues/339)) ([f5bbac2](https://github.com/jbcom/radioactive-ralph/commit/f5bbac2f4f86434d9079d7ad7967f9e9a4308c87))
* **cli:** list tasks in status output, and make the meso row readable ([#305](https://github.com/jbcom/radioactive-ralph/issues/305)) ([2d6cf70](https://github.com/jbcom/radioactive-ralph/commit/2d6cf70d06fbe8e73eca9109dd7ba4e2d150925a))
* **cli:** say why a failed task failed ([#308](https://github.com/jbcom/radioactive-ralph/issues/308)) ([fba85ce](https://github.com/jbcom/radioactive-ralph/commit/fba85ce8bf81bcb2487ac3e67f48ff587c419ed5))
* **contain:** default provider write containment ON ([#292](https://github.com/jbcom/radioactive-ralph/issues/292)) ([881c381](https://github.com/jbcom/radioactive-ralph/commit/881c3810a4ecc0e0c63d139a788cfb21d9a25900))
* **contain:** kernel-enforced write containment for provider processes (macOS + Linux) ([#251](https://github.com/jbcom/radioactive-ralph/issues/251)) ([2311009](https://github.com/jbcom/radioactive-ralph/commit/23110099c3db5275280c85401f227a61111054e5))
* **contain:** per-provider write allowances ([#296](https://github.com/jbcom/radioactive-ralph/issues/296) first half) ([#297](https://github.com/jbcom/radioactive-ralph/issues/297)) ([a22c847](https://github.com/jbcom/radioactive-ralph/commit/a22c8470f6a6c6bdc91ec1ff29ca253819c5e8d9))
* **ipc:** let an operator record a provider calibration ([#263](https://github.com/jbcom/radioactive-ralph/issues/263)) ([9fe7d33](https://github.com/jbcom/radioactive-ralph/commit/9fe7d33bd249aeafaa092108364d470ec40e46a6))
* **observe:** flag a plan that has no runnable work left ([#310](https://github.com/jbcom/radioactive-ralph/issues/310)) ([c865b98](https://github.com/jbcom/radioactive-ralph/commit/c865b989c6c669ccc843ee3234e63c163fb8a6a7))
* **observe:** follow the dependency chain to the root failure ([#309](https://github.com/jbcom/radioactive-ralph/issues/309)) ([656496b](https://github.com/jbcom/radioactive-ralph/commit/656496bb8bc548f06aee76af5195c7dbc4535893))
* **observe:** name the dependency that makes a task unreachable ([#307](https://github.com/jbcom/radioactive-ralph/issues/307)) ([59150e9](https://github.com/jbcom/radioactive-ralph/commit/59150e9b38a8a37fce8550688906eb29a76aabf9))
* **observe:** project per-task provenance and ready-partition identity ([#304](https://github.com/jbcom/radioactive-ralph/issues/304)) ([6ea0489](https://github.com/jbcom/radioactive-ralph/commit/6ea0489c2a87f81c3023c5ba31fd455a0057b111))
* **operator:** name the reclaim cause on the task row ([#323](https://github.com/jbcom/radioactive-ralph/issues/323)) ([ecf856f](https://github.com/jbcom/radioactive-ralph/commit/ecf856fe9a89a44f4fc3d037d3e96498e1fb44a4))
* **orch:** dispatch walks the dependency graph ([#225](https://github.com/jbcom/radioactive-ralph/issues/225)) ([5f32231](https://github.com/jbcom/radioactive-ralph/commit/5f3223131a0ac0c82a0b90fccfd3cb364f753d59))
* **orch:** enforce differentFrom at dispatch ([#272](https://github.com/jbcom/radioactive-ralph/issues/272)) ([bf77756](https://github.com/jbcom/radioactive-ralph/commit/bf777569ce4ce8740006f4cb7f8431973535b176))
* **orch:** name a verification timeout instead of leaking a bare deadline ([#335](https://github.com/jbcom/radioactive-ralph/issues/335)) ([772cfca](https://github.com/jbcom/radioactive-ralph/commit/772cfcae0b4cf5627077b34e074e0d23d66ce1f1))
* **orch:** record concurrent load on a failed turn ([#342](https://github.com/jbcom/radioactive-ralph/issues/342)) ([5d4a1f3](https://github.com/jbcom/radioactive-ralph/commit/5d4a1f36f67e9b8f168331b5660c54bee45b660b))
* **orch:** record what each task actually ran on ([#262](https://github.com/jbcom/radioactive-ralph/issues/262)) ([10f1b51](https://github.com/jbcom/radioactive-ralph/commit/10f1b5170416f4e5ab71783e1dcda6cd5969d8a2))
* **orch:** report dispatch-slot saturation instead of returning silently ([#257](https://github.com/jbcom/radioactive-ralph/issues/257)) ([05f547b](https://github.com/jbcom/radioactive-ralph/commit/05f547b4cb62540160adcf384d58f4962d34005d))
* **orch:** wire the decision log into dispatch ([#336](https://github.com/jbcom/radioactive-ralph/issues/336)) ([a174f1e](https://github.com/jbcom/radioactive-ralph/commit/a174f1e52e49973b1c676317e0ab82242c4b71c6))
* **plan:** enforce a declared per-task binding at dispatch ([#283](https://github.com/jbcom/radioactive-ralph/issues/283)) ([fab28dd](https://github.com/jbcom/radioactive-ralph/commit/fab28dd5ee7aca5e33f8fce9ff7037eeaa612284))
* **plan:** validate differentFrom references at import ([#259](https://github.com/jbcom/radioactive-ralph/issues/259)) ([fde2352](https://github.com/jbcom/radioactive-ralph/commit/fde2352090b9082e091d2c481d3a39e39dc6f80e))
* provider cooldown tracking, shell env inheritance, and binding model overrides ([#380](https://github.com/jbcom/radioactive-ralph/issues/380)) ([1bdae62](https://github.com/jbcom/radioactive-ralph/commit/1bdae6252b8e2793381ab3c8fa9556352778f62a))
* **provider:** classify what an interactive block asked for ([#347](https://github.com/jbcom/radioactive-ralph/issues/347)) ([0f53c8b](https://github.com/jbcom/radioactive-ralph/commit/0f53c8b4b1437eb386dea543033f200013a058e4))
* **provider:** declare per-binding containment capability, and honor it ([#290](https://github.com/jbcom/radioactive-ralph/issues/290)) ([ee6b1f8](https://github.com/jbcom/radioactive-ralph/commit/ee6b1f835d286bf48ac2bef835fd0af923fdfa9e))
* **provider:** declare per-provider write paths; codex and opencode run contained ([#298](https://github.com/jbcom/radioactive-ralph/issues/298)) ([23694c3](https://github.com/jbcom/radioactive-ralph/commit/23694c3292d8bfc526841c80e257931d13d79c81))
* **scripts:** a real self-test that has Ralph verify Ralph ([#311](https://github.com/jbcom/radioactive-ralph/issues/311)) ([0daabd9](https://github.com/jbcom/radioactive-ralph/commit/0daabd9c7bd1fab12c567c267e033dd25b7ff1e8))
* **store:** add DeletePlan, the retention primitive accumulation needs ([#319](https://github.com/jbcom/radioactive-ralph/issues/319)) ([9df0b85](https://github.com/jbcom/radioactive-ralph/commit/9df0b85a5093fcc5c2314c10817002a7c04e9656))
* **store:** decide that reclaims do not consume retry budget ([#328](https://github.com/jbcom/radioactive-ralph/issues/328)) ([9e5838e](https://github.com/jbcom/radioactive-ralph/commit/9e5838e9d1538d2dac56bfc5e934a63f2272d09b))
* **store:** partition a ready wave by declared binding, not group alone ([#282](https://github.com/jbcom/radioactive-ralph/issues/282)) ([c621409](https://github.com/jbcom/radioactive-ralph/commit/c62140998c05d6158e0ba5654f2f6834df9c3751))
* **store:** report attempts a reclaimed task actually got ([#327](https://github.com/jbcom/radioactive-ralph/issues/327)) ([4f648a7](https://github.com/jbcom/radioactive-ralph/commit/4f648a7e15e5b7acdb1e7998932195a54ec77ea6))


### Bug Fixes

* **agent:** bound session cleanup in wall-clock time, not poll attempts ([#275](https://github.com/jbcom/radioactive-ralph/issues/275)) ([adf21cd](https://github.com/jbcom/radioactive-ralph/commit/adf21cdbe84b7af30c18ce56c5d8e5e71abdc5a7)), closes [#273](https://github.com/jbcom/radioactive-ralph/issues/273)
* **agent:** stop reporting a reaped process as a leaked session member ([#364](https://github.com/jbcom/radioactive-ralph/issues/364)) ([435b049](https://github.com/jbcom/radioactive-ralph/commit/435b049471b71cf2e8a636be1dc6215481838d13))
* **ci:** survive a stale disk image when creating the DMG ([#361](https://github.com/jbcom/radioactive-ralph/issues/361)) ([948afe3](https://github.com/jbcom/radioactive-ralph/commit/948afe379b1d417605937a9147ce077e9f2b2163))
* enforce OpenCode completion through managed launcher ([#379](https://github.com/jbcom/radioactive-ralph/issues/379)) ([2b4e27a](https://github.com/jbcom/radioactive-ralph/commit/2b4e27aa4aefffb2d522fdaf45818020fa084c56))
* **observe:** only partition a task that is actually dispatchable ([#306](https://github.com/jbcom/radioactive-ralph/issues/306)) ([c6ccb02](https://github.com/jbcom/radioactive-ralph/commit/c6ccb02b10c62ab399508a2ce91bc9823c7eae10))
* **orch:** refuse the real state root under test ([#353](https://github.com/jbcom/radioactive-ralph/issues/353)) ([ba561db](https://github.com/jbcom/radioactive-ralph/commit/ba561db181caa3a6176b882583223bf2e508c30f))
* **packaging:** retry the appimagetool download, never the verification ([#270](https://github.com/jbcom/radioactive-ralph/issues/270)) ([0cd0166](https://github.com/jbcom/radioactive-ralph/commit/0cd0166bcaed20fade11171aa0a0844ba999ca90))
* **provider:** categorize a claude failure that carries no api_error_status ([#278](https://github.com/jbcom/radioactive-ralph/issues/278)) ([e42e3ab](https://github.com/jbcom/radioactive-ralph/commit/e42e3ab762becb69a808cce8331fdd26968a644a))
* **provider:** match a question asked, not a word mentioned ([#356](https://github.com/jbcom/radioactive-ralph/issues/356)) ([49817c4](https://github.com/jbcom/radioactive-ralph/commit/49817c4fcd09b5b946ddd20cd99f57763df96e56))
* **provider:** send `message`, not `Message`, on claude's stream-json stdin ([#281](https://github.com/jbcom/radioactive-ralph/issues/281)) ([94b7955](https://github.com/jbcom/radioactive-ralph/commit/94b79553bb2a1ce7646d4a79bd345dbe91f618ff))
* **review:** render the attempt label outside the reclaim branch ([#332](https://github.com/jbcom/radioactive-ralph/issues/332)) ([8dcac37](https://github.com/jbcom/radioactive-ralph/commit/8dcac37719309b2851698d1ce9067ee80176d1a3))
* **scripts:** do not count a queue-superseded failure as actionable ([#271](https://github.com/jbcom/radioactive-ralph/issues/271)) ([28adeff](https://github.com/jbcom/radioactive-ralph/commit/28adeff515828c3b0c761adbb8200ab7f26e6537))
* **scripts:** re-check a failing PR before halting the driver ([#268](https://github.com/jbcom/radioactive-ralph/issues/268)) ([5c5159a](https://github.com/jbcom/radioactive-ralph/commit/5c5159a2ed69d6098db86954f2e43d1aac79b942))
* **self-test:** import a fresh run each time, not a stale one ([#312](https://github.com/jbcom/radioactive-ralph/issues/312)) ([f2e622d](https://github.com/jbcom/radioactive-ralph/commit/f2e622dc4c17013255de3cb439a244aa4a906ecd))
* **self-test:** renew the progress lease on long silent steps ([#322](https://github.com/jbcom/radioactive-ralph/issues/322)) ([c65e1af](https://github.com/jbcom/radioactive-ralph/commit/c65e1affe07f4b75471d6599815cd1fe368f1099))
* **self-test:** report tracked edits even when the run fails ([#316](https://github.com/jbcom/radioactive-ralph/issues/316)) ([07a469c](https://github.com/jbcom/radioactive-ralph/commit/07a469c9667bbb9d06c4270cd08ea310a58d8347))
* tighten prompt detection and API contracts ([#376](https://github.com/jbcom/radioactive-ralph/issues/376)) ([5df8ae5](https://github.com/jbcom/radioactive-ralph/commit/5df8ae5b404313f8db98f0413a410bfe6d0884f5))

## [0.33.1](https://github.com/jbcom/radioactive-ralph/compare/v0.33.0...v0.33.1) (2026-07-28)


### Bug Fixes

* **scripts:** rebase one behind PR per round, not all of them ([#265](https://github.com/jbcom/radioactive-ralph/issues/265)) ([b9313db](https://github.com/jbcom/radioactive-ralph/commit/b9313dba9988a6e3e040a13fd21aa3233d986c2d))

## [0.33.0](https://github.com/jbcom/radioactive-ralph/compare/v0.32.0...v0.33.0) (2026-07-28)


### Features

* **observe:** surface why a task is blocked, and stop blocked tasks blanking the snapshot ([#258](https://github.com/jbcom/radioactive-ralph/issues/258)) ([c94ca79](https://github.com/jbcom/radioactive-ralph/commit/c94ca790d55874b37031f4c71a25c2d7e4748471))

## [0.32.0](https://github.com/jbcom/radioactive-ralph/compare/v0.31.0...v0.32.0) (2026-07-28)


### Features

* **cli:** move init behind the supervisor (closes [#204](https://github.com/jbcom/radioactive-ralph/issues/204) criterion 1) ([#245](https://github.com/jbcom/radioactive-ralph/issues/245)) ([c3f8fa5](https://github.com/jbcom/radioactive-ralph/commit/c3f8fa53e6d9ba00d450017d6e4ce18f55ab4747))
* **cli:** resolve the desktop launch through the supervisor ([#246](https://github.com/jbcom/radioactive-ralph/issues/246)) ([778778c](https://github.com/jbcom/radioactive-ralph/commit/778778c7ee91466902333752007fcd90211ad9d8))
* **orch:** enforce a task's declared capability requirements ([#247](https://github.com/jbcom/radioactive-ralph/issues/247)) ([82ff030](https://github.com/jbcom/radioactive-ralph/commit/82ff03094b56c42a71faf5ff4ce89972d80cc8a3))
* **store:** calibration records and attempts ([#236](https://github.com/jbcom/radioactive-ralph/issues/236)) ([9c04550](https://github.com/jbcom/radioactive-ralph/commit/9c04550dfc8f42a6ce7828f1c345e662153844d1))


### Bug Fixes

* **orch:** attribute evidence to the reporting session, not the current owner ([#256](https://github.com/jbcom/radioactive-ralph/issues/256)) ([0dec020](https://github.com/jbcom/radioactive-ralph/commit/0dec0205e911635a5b3ac9fd96e9da250dad9672))

## [0.31.0](https://github.com/jbcom/radioactive-ralph/compare/v0.30.0...v0.31.0) (2026-07-28)


### Features

* **provider:** resolve and report the actual invocation ([#234](https://github.com/jbcom/radioactive-ralph/issues/234)) ([7f0b481](https://github.com/jbcom/radioactive-ralph/commit/7f0b481ae9eb2aa0b884d60153a48fc929d378d8))


### Bug Fixes

* **ipc:** retire the accept loop before closing the listener ([#224](https://github.com/jbcom/radioactive-ralph/issues/224)) ([c79c620](https://github.com/jbcom/radioactive-ralph/commit/c79c6201ea07c0e25e143e27f565f63b9efdf64a))

## [0.30.0](https://github.com/jbcom/radioactive-ralph/compare/v0.29.1...v0.30.0) (2026-07-28)


### Features

* **ipc:** add the project-config surface ([#204](https://github.com/jbcom/radioactive-ralph/issues/204) criterion 1, part 1) ([#233](https://github.com/jbcom/radioactive-ralph/issues/233)) ([ca181ba](https://github.com/jbcom/radioactive-ralph/commit/ca181bace389e0ad0ab0452a925848c74e3cb111))

## [0.29.1](https://github.com/jbcom/radioactive-ralph/compare/v0.29.0...v0.29.1) (2026-07-27)


### Bug Fixes

* **ci:** give each packaging rollback case a unique dir ([#226](https://github.com/jbcom/radioactive-ralph/issues/226)) ([051ed99](https://github.com/jbcom/radioactive-ralph/commit/051ed994f9ea496611eb4f77bc4417128432a9b4))

## [0.29.0](https://github.com/jbcom/radioactive-ralph/compare/v0.28.0...v0.29.0) (2026-07-27)


### Features

* **provider:** add --no-chrome and --pure; REFUSE the permission bypasses ([#235](https://github.com/jbcom/radioactive-ralph/issues/235)) ([f0d4a4b](https://github.com/jbcom/radioactive-ralph/commit/f0d4a4be7059447a87334bb254c9d25486fb8d17))

## [0.28.0](https://github.com/jbcom/radioactive-ralph/compare/v0.27.0...v0.28.0) (2026-07-27)


### Features

* **provider:** classify claude failures (closes [#204](https://github.com/jbcom/radioactive-ralph/issues/204) criterion 3) ([#230](https://github.com/jbcom/radioactive-ralph/issues/230)) ([854362d](https://github.com/jbcom/radioactive-ralph/commit/854362d31b3f7000130392a4fb4a713b43846e2a))

## [0.27.0](https://github.com/jbcom/radioactive-ralph/compare/v0.26.0...v0.27.0) (2026-07-27)


### Features

* **cli:** move plan listing and project identity behind the supervisor ([#220](https://github.com/jbcom/radioactive-ralph/issues/220)) ([eb58b03](https://github.com/jbcom/radioactive-ralph/commit/eb58b037517abfd4d6c254a33eaddf5d0cbddaef))

## [0.26.0](https://github.com/jbcom/radioactive-ralph/compare/v0.25.0...v0.26.0) (2026-07-27)


### Features

* import plans as graphs — the linear/DAG keystone ([#221](https://github.com/jbcom/radioactive-ralph/issues/221)) ([9ba6096](https://github.com/jbcom/radioactive-ralph/commit/9ba6096103ace682be6a1a2b588d93b55d88d55e))


### Bug Fixes

* **ipc:** drop host identifiers from StatusReply (closes [#204](https://github.com/jbcom/radioactive-ralph/issues/204) criterion 2) ([#231](https://github.com/jbcom/radioactive-ralph/issues/231)) ([2eb8d24](https://github.com/jbcom/radioactive-ralph/commit/2eb8d24ec3f0b3136c9a123154974587f204a7ed))

## [0.25.0](https://github.com/jbcom/radioactive-ralph/compare/v0.24.0...v0.25.0) (2026-07-27)


### Features

* **store:** add ClaimTask, the exact named claim ([#216](https://github.com/jbcom/radioactive-ralph/issues/216)) ([561f93e](https://github.com/jbcom/radioactive-ralph/commit/561f93eadc6a5f10e21bb4d9eecaf7f0cc86c461))
* **store:** plan-graph task metadata and provenance ([#215](https://github.com/jbcom/radioactive-ralph/issues/215)) ([bdda55e](https://github.com/jbcom/radioactive-ralph/commit/bdda55e38820e8f053d108e86f18a33c8a5bb89f))

## [0.24.0](https://github.com/jbcom/radioactive-ralph/compare/v0.23.0...v0.24.0) (2026-07-27)


### Features

* **observe:** machine-readable operator state (partial [#204](https://github.com/jbcom/radioactive-ralph/issues/204)) ([#219](https://github.com/jbcom/radioactive-ralph/issues/219)) ([7f79c11](https://github.com/jbcom/radioactive-ralph/commit/7f79c11bf8aaed481fd7cef6c9278dc26fb278fb))

## [0.23.0](https://github.com/jbcom/radioactive-ralph/compare/v0.22.4...v0.23.0) (2026-07-27)


### Features

* **plan:** parse explicit dependency edges from step metadata ([#217](https://github.com/jbcom/radioactive-ralph/issues/217)) ([736e31d](https://github.com/jbcom/radioactive-ralph/commit/736e31d7f5b800cc47c46a42b5b11dbfcd74b4fc))

## [0.22.4](https://github.com/jbcom/radioactive-ralph/compare/v0.22.3...v0.22.4) (2026-07-27)


### Bug Fixes

* **store:** make concurrent first-open safe ([#212](https://github.com/jbcom/radioactive-ralph/issues/212)) ([72b75b5](https://github.com/jbcom/radioactive-ralph/commit/72b75b55f25404a8fbaf77af4293c1ec6d81e721))

## [0.22.3](https://github.com/jbcom/radioactive-ralph/compare/v0.22.2...v0.22.3) (2026-07-27)


### Bug Fixes

* **provider:** separate turn deadlines from stall detection and contain provider process trees ([#209](https://github.com/jbcom/radioactive-ralph/issues/209)) ([3045d07](https://github.com/jbcom/radioactive-ralph/commit/3045d0768733c39b68035a1dcc0bef09dcc6d002))

## [0.22.2](https://github.com/jbcom/radioactive-ralph/compare/v0.22.1...v0.22.2) (2026-07-26)


### Bug Fixes

* **release:** allow admission to view private drafts ([#208](https://github.com/jbcom/radioactive-ralph/issues/208)) ([537add1](https://github.com/jbcom/radioactive-ralph/commit/537add157d6a282be9f52d2d745b593b5234e06a))

## [0.22.1](https://github.com/jbcom/radioactive-ralph/compare/v0.22.0...v0.22.1) (2026-07-26)


### Bug Fixes

* **release:** isolate immutable settings authority ([#206](https://github.com/jbcom/radioactive-ralph/issues/206)) ([66c3ee5](https://github.com/jbcom/radioactive-ralph/commit/66c3ee528ce39b25e76e0c03483bf82c4dd7fd24))

## [0.22.0](https://github.com/jbcom/radioactive-ralph/compare/v0.21.6...v0.22.0) (2026-07-26)

### Windows support boundary

Native Windows SCM installation and service start are intentionally disabled
and fail before mutating the service definition or configuration. Native
foreground mode supports the supervisor/client control plane only; provider
workers return `ErrPTYUnsupported`. Use WSL2 with `systemd --user` for
functional provider-backed execution. Native `service status` and
`service uninstall` remain remediation-only commands for safely inspecting or
removing a prior development registration.

### Supervisor behavior

Ralph now rejects ambiguous plans, selects providers atomically, validates
backend-specific paths, reconciles service lifecycle state, and enforces
configured worker ceilings after admission.


### Features

* apply attach deltas — the live view goes push-live ([#173](https://github.com/jbcom/radioactive-ralph/issues/173)) ([594dfd5](https://github.com/jbcom/radioactive-ralph/commit/594dfd50609693ad2107a15cf42d0225e8029ba2))
* **cli:** add 'radioactive_ralph events' — headless live event tail ([#178](https://github.com/jbcom/radioactive-ralph/issues/178)) ([4211ee2](https://github.com/jbcom/radioactive-ralph/commit/4211ee2d7db3acfb1a9ec14b45f39824775612e3))
* **plan:** gate a step behind operator approval with the [approval] marker ([#154](https://github.com/jbcom/radioactive-ralph/issues/154)) ([ad72877](https://github.com/jbcom/radioactive-ralph/commit/ad7287749ada47118d3aa75f512475e34c6c3ff3))
* stream events over Attach — turn the observe half live ([#169](https://github.com/jbcom/radioactive-ralph/issues/169)) ([71c205b](https://github.com/jbcom/radioactive-ralph/commit/71c205b288b65340bf28bd243d34e5424608aa85))
* **supervisor:** add observable provider pools ([#193](https://github.com/jbcom/radioactive-ralph/issues/193)) ([04d1871](https://github.com/jbcom/radioactive-ralph/commit/04d18710c6ea97124cee411537d68f818183218c))
* **tui:** cursor-aware reconnect — no macro event missed across a blip ([#184](https://github.com/jbcom/radioactive-ralph/issues/184)) ([1cf3e2c](https://github.com/jbcom/radioactive-ralph/commit/1cf3e2c64979c71cd5c940b6450bac9246193fef))
* **tui:** live macro plan-PROGRESS deltas ([#188](https://github.com/jbcom/radioactive-ralph/issues/188)) ([a25ed2e](https://github.com/jbcom/radioactive-ralph/commit/a25ed2e0139e23e77664fde8213a560cfa5fff09))
* **tui:** session-long live event tail — macro/meso go push-live ([#182](https://github.com/jbcom/radioactive-ralph/issues/182)) ([7e4004a](https://github.com/jbcom/radioactive-ralph/commit/7e4004adf52f0f8494738fa1dab81fe77848e217))


### Bug Fixes

* **actions:** harden dependency auto-merge ([#194](https://github.com/jbcom/radioactive-ralph/issues/194)) ([394acab](https://github.com/jbcom/radioactive-ralph/commit/394acab60786f218cacc9052187e83d2d04c6531))
* **agent:** kill via exec's coordinated Cancel; no spurious stall on zero timeout ([#156](https://github.com/jbcom/radioactive-ralph/issues/156)) ([82592a3](https://github.com/jbcom/radioactive-ralph/commit/82592a3a302ff94b9b579e68703ae99acd620895))
* **gui:** isolate desktop launch in root tests ([#200](https://github.com/jbcom/radioactive-ralph/issues/200)) ([eda2bf8](https://github.com/jbcom/radioactive-ralph/commit/eda2bf81b4098b0eb4c01c94280a17a5804c949c))
* **gui:** keep lifecycle work on the live event loop ([#201](https://github.com/jbcom/radioactive-ralph/issues/201)) ([835fc2b](https://github.com/jbcom/radioactive-ralph/commit/835fc2bc298686ec7cad74b9cfc41fee913a49a5))
* **gui:** reconnect the live event stream instead of dying after one end ([#164](https://github.com/jbcom/radioactive-ralph/issues/164)) ([58ab5ab](https://github.com/jbcom/radioactive-ralph/commit/58ab5abc8c1f6b130fc896dfa0e2568fb198103b))
* **ipc:** bound reads/writes + close conns on Stop so a bad client can't wedge the server ([#160](https://github.com/jbcom/radioactive-ralph/issues/160)) ([68df639](https://github.com/jbcom/radioactive-ralph/commit/68df639c718d55bfd882c1cf6b5c21f34fffe485))
* **ipc:** detect Attach client disconnect so its handler doesn't leak ([#165](https://github.com/jbcom/radioactive-ralph/issues/165)) ([457930f](https://github.com/jbcom/radioactive-ralph/commit/457930f15c7f00eebd2b0d464dee07f717785fb8))
* **orch:** contain a panicking dispatch turn instead of crashing the supervisor ([#146](https://github.com/jbcom/radioactive-ralph/issues/146)) ([66a780c](https://github.com/jbcom/radioactive-ralph/commit/66a780c96ae1666c1fcb9ee240754d7bbf16d588))
* **provider:** fail a codex turn when the CLI exits nonzero ([#152](https://github.com/jbcom/radioactive-ralph/issues/152)) ([0fb53f1](https://github.com/jbcom/radioactive-ralph/commit/0fb53f12b4ae5973af06b05bff0945651181bdf2))
* **provider:** fail oversized stream-json turns after reaping the provider process tree ([ab5faa9](https://github.com/jbcom/radioactive-ralph/commit/ab5faa9c7328abf3a54a6ec18a6051f6f4d7a139))
* **provider:** make agent lifecycle and results fail closed ([#202](https://github.com/jbcom/radioactive-ralph/issues/202)) ([eb04193](https://github.com/jbcom/radioactive-ralph/commit/eb04193715d2792b45cc20b0a68e2df70075b9c9))
* **release:** make native publication atomic and verifiable ([#199](https://github.com/jbcom/radioactive-ralph/issues/199)) ([c6eb5d1](https://github.com/jbcom/radioactive-ralph/commit/c6eb5d1f85db4e1396bfb61650d8a4add51d403d))
* **security:** retire archival dependency surfaces ([#195](https://github.com/jbcom/radioactive-ralph/issues/195)) ([5dbbbcc](https://github.com/jbcom/radioactive-ralph/commit/5dbbbccf72080d244051790612e7ae0d3fb84ad7))
* **service:** harden supervisor install defaults ([#203](https://github.com/jbcom/radioactive-ralph/issues/203)) ([99b4b06](https://github.com/jbcom/radioactive-ralph/commit/99b4b0663fd7ddf210964c2546e87a395a55d409))
* **store:** make an approved 'ready' task claimable (close the approval-gate dead-end) ([#147](https://github.com/jbcom/radioactive-ralph/issues/147)) ([dba8192](https://github.com/jbcom/radioactive-ralph/commit/dba8192bda4f86004f234fe7f165407fe29aaedc))
* **store:** make payload_json always valid JSON structurally ([#175](https://github.com/jbcom/radioactive-ralph/issues/175)) ([c438d1a](https://github.com/jbcom/radioactive-ralph/commit/c438d1a83b476636da4adc04468875f0a4a1394c))
* **store:** stop the reaper double-executing a long-running worker's task ([#149](https://github.com/jbcom/radioactive-ralph/issues/149)) ([bb2d624](https://github.com/jbcom/radioactive-ralph/commit/bb2d624862bdd66356e9458a8309f6f83816e861))
* **tui:** preserve selected entity across refreshes; track all gathers ([#157](https://github.com/jbcom/radioactive-ralph/issues/157)) ([f01764a](https://github.com/jbcom/radioactive-ralph/commit/f01764a6cf262fe382a69dafa9d1957956ce15aa))

## [0.21.6](https://github.com/jbcom/radioactive-ralph/compare/v0.21.5...v0.21.6) (2026-07-17)


### Bug Fixes

* **provider:** bound declarative turns + reap their process tree ([#139](https://github.com/jbcom/radioactive-ralph/issues/139)) ([925d4db](https://github.com/jbcom/radioactive-ralph/commit/925d4dbeec739868fdb4848b1bd50a25e6bf26c4))

## [0.21.5](https://github.com/jbcom/radioactive-ralph/compare/v0.21.4...v0.21.5) (2026-07-17)


### Bug Fixes

* **agent:** kill the whole process group, not just the direct child ([#138](https://github.com/jbcom/radioactive-ralph/issues/138)) ([2847d64](https://github.com/jbcom/radioactive-ralph/commit/2847d641f5acbfd82f57b1863834e8c24604f94f))

## [0.21.4](https://github.com/jbcom/radioactive-ralph/compare/v0.21.3...v0.21.4) (2026-07-17)


### Bug Fixes

* **agent:** stop the watchdog goroutine after a stall (goroutine leak) ([#136](https://github.com/jbcom/radioactive-ralph/issues/136)) ([e24aa51](https://github.com/jbcom/radioactive-ralph/commit/e24aa51a242aff478fa9ea8a53c62625a31e27b7))

## [0.21.3](https://github.com/jbcom/radioactive-ralph/compare/v0.21.2...v0.21.3) (2026-07-17)


### Bug Fixes

* **orch:** reserve capped-provider spend before concurrent launches ([#131](https://github.com/jbcom/radioactive-ralph/issues/131)) ([637d8dd](https://github.com/jbcom/radioactive-ralph/commit/637d8dd3ab88e5411b84fd778761b213ff047379))

## [0.21.2](https://github.com/jbcom/radioactive-ralph/compare/v0.21.1...v0.21.2) (2026-07-17)


### Bug Fixes

* **store:** cap the SQLite pool at one connection ([#129](https://github.com/jbcom/radioactive-ralph/issues/129)) ([531f9c7](https://github.com/jbcom/radioactive-ralph/commit/531f9c7963651badf3b0ebdcb500dab90262f1dd))

## [0.21.1](https://github.com/jbcom/radioactive-ralph/compare/v0.21.0...v0.21.1) (2026-07-17)


### Bug Fixes

* **orch:** dispatch provider turns asynchronously (never-block invariant) ([#127](https://github.com/jbcom/radioactive-ralph/issues/127)) ([f4627eb](https://github.com/jbcom/radioactive-ralph/commit/f4627ebb9074720191c1a08b623c865d778631bb))

## [0.21.0](https://github.com/jbcom/radioactive-ralph/compare/v0.20.0...v0.21.0) (2026-07-17)


### Features

* **gui:** confirm before abandoning a plan or killing a worker ([#122](https://github.com/jbcom/radioactive-ralph/issues/122)) ([1b23218](https://github.com/jbcom/radioactive-ralph/commit/1b23218ff811e5f097bc9d45330d2413e0b9fbb8))


### Bug Fixes

* **doctor:** distinguish missing claude CLI from unauthenticated ([#125](https://github.com/jbcom/radioactive-ralph/issues/125)) ([5e7ef40](https://github.com/jbcom/radioactive-ralph/commit/5e7ef403110f0db924c0399839439167b4bff694))
* **gui:** scroll to top when the drill view changes ([#123](https://github.com/jbcom/radioactive-ralph/issues/123)) ([6ce981d](https://github.com/jbcom/radioactive-ralph/commit/6ce981de211149d9ae97f819ab06f554d7ebc2ea))

## [0.20.0](https://github.com/jbcom/radioactive-ralph/compare/v0.19.1...v0.20.0) (2026-07-17)


### Features

* **doctor:** verify the XDG state root is usable ([#120](https://github.com/jbcom/radioactive-ralph/issues/120)) ([997f701](https://github.com/jbcom/radioactive-ralph/commit/997f7018d75a09f58d6716feabfd3c28699496a7))

## [0.19.1](https://github.com/jbcom/radioactive-ralph/compare/v0.19.0...v0.19.1) (2026-07-17)


### Bug Fixes

* **gui:** coordinate drive-action errors with the paint loop ([#119](https://github.com/jbcom/radioactive-ralph/issues/119)) ([9788d20](https://github.com/jbcom/radioactive-ralph/commit/9788d20669a86e33d37b504a81d41483c1ae9c72))

## [0.19.0](https://github.com/jbcom/radioactive-ralph/compare/v0.18.0...v0.19.0) (2026-07-17)


### Features

* **gui:** focus the first action after each drill render (a11y) ([#116](https://github.com/jbcom/radioactive-ralph/issues/116)) ([e7c473f](https://github.com/jbcom/radioactive-ralph/commit/e7c473f6cf60843ec82e6552412b09a6ae78e468))

## [0.18.0](https://github.com/jbcom/radioactive-ralph/compare/v0.17.1...v0.18.0) (2026-07-17)


### Features

* **doctor:** surface the codex spend-cap metering blind spot ([#112](https://github.com/jbcom/radioactive-ralph/issues/112)) ([cacdc63](https://github.com/jbcom/radioactive-ralph/commit/cacdc631d83a1d068d94fc1284017b421a202479))

## [0.17.1](https://github.com/jbcom/radioactive-ralph/compare/v0.17.0...v0.17.1) (2026-07-17)


### Bug Fixes

* **ci:** pin a well-formed locale for the GUI test (Fyne harfbuzz panic) ([#110](https://github.com/jbcom/radioactive-ralph/issues/110)) ([49f78d1](https://github.com/jbcom/radioactive-ralph/commit/49f78d15c56b13c650e853fe943e5d924d4f56cc))

## [0.17.0](https://github.com/jbcom/radioactive-ralph/compare/v0.16.2...v0.17.0) (2026-07-17)


### Features

* **gui:** Escape-to-drill-back keyboard navigation ([#107](https://github.com/jbcom/radioactive-ralph/issues/107)) ([98dcce6](https://github.com/jbcom/radioactive-ralph/commit/98dcce6ab1cbec5856a6b1e8b2d75d9dac6d9978))

## [0.16.2](https://github.com/jbcom/radioactive-ralph/compare/v0.16.1...v0.16.2) (2026-07-17)


### Bug Fixes

* **packaging:** macOS cask ships both arm64 + amd64 (Intel Macs) ([#106](https://github.com/jbcom/radioactive-ralph/issues/106)) ([68ca1e6](https://github.com/jbcom/radioactive-ralph/commit/68ca1e6a6512e9cdd420336f1db4c1d99cbd4aaf))

## [0.16.1](https://github.com/jbcom/radioactive-ralph/compare/v0.16.0...v0.16.1) (2026-07-17)


### Bug Fixes

* **gui,packaging:** AppImage FUSE, project-agnostic import, stale-paint, count labels ([#102](https://github.com/jbcom/radioactive-ralph/issues/102)) ([860ad08](https://github.com/jbcom/radioactive-ralph/commit/860ad08b521c2655f639d70dd734234e81e88147))

## [0.16.0](https://github.com/jbcom/radioactive-ralph/compare/v0.15.0...v0.16.0) (2026-07-17)


### Features

* **tui:** supervisor-liveness line in the macro header ([#98](https://github.com/jbcom/radioactive-ralph/issues/98)) ([a0addbe](https://github.com/jbcom/radioactive-ralph/commit/a0addbe23337e6ff059564ff60d58b360d4c2d1f))

## [0.15.0](https://github.com/jbcom/radioactive-ralph/compare/v0.14.0...v0.15.0) (2026-07-17)


### Features

* **gui:** recent-activity project-events feed (TUI parity) ([#96](https://github.com/jbcom/radioactive-ralph/issues/96)) ([ad99986](https://github.com/jbcom/radioactive-ralph/commit/ad9998699886656e2164cfa99771af2cf428c63d))

## [0.14.0](https://github.com/jbcom/radioactive-ralph/compare/v0.13.0...v0.14.0) (2026-07-17)


### Features

* native installers & GUI desktop packaging ([#92](https://github.com/jbcom/radioactive-ralph/issues/92)) ([a1df782](https://github.com/jbcom/radioactive-ralph/commit/a1df782295d6bf9fdadba8fd157633bc0b057eb3))

## [0.13.0](https://github.com/jbcom/radioactive-ralph/compare/v0.12.0...v0.13.0) (2026-07-17)


### Features

* Fyne desktop GUI client ([#89](https://github.com/jbcom/radioactive-ralph/issues/89)) ([e969551](https://github.com/jbcom/radioactive-ralph/commit/e969551bbb93a5cc929f36e8eb84ceb23c67c33b))

## [0.12.0](https://github.com/jbcom/radioactive-ralph/compare/v0.11.0...v0.12.0) (2026-07-17)


### Features

* versioned IPC drive+observe API ([#87](https://github.com/jbcom/radioactive-ralph/issues/87)) ([2f20adf](https://github.com/jbcom/radioactive-ralph/commit/2f20adfa36373df9cb03aa54e6129dba75553761))

## [0.11.0](https://github.com/jbcom/radioactive-ralph/compare/v0.10.4...v0.11.0) (2026-07-17)


### Features

* guided first-run onboarding wizard ([#85](https://github.com/jbcom/radioactive-ralph/issues/85)) ([80daad9](https://github.com/jbcom/radioactive-ralph/commit/80daad9cf480df90fda322240e4269cba0befc29))

## [0.10.4](https://github.com/jbcom/radioactive-ralph/compare/v0.10.3...v0.10.4) (2026-07-17)


### Bug Fixes

* cassette pump-join — final audit convergence fix ([#83](https://github.com/jbcom/radioactive-ralph/issues/83)) ([eedb6d3](https://github.com/jbcom/radioactive-ralph/commit/eedb6d3555f5b13c9b582f47f660b12d44684473))

## [0.10.3](https://github.com/jbcom/radioactive-ralph/compare/v0.10.2...v0.10.3) (2026-07-17)


### Bug Fixes

* resolve all 5 findings from the third convergence audit ([#81](https://github.com/jbcom/radioactive-ralph/issues/81)) ([a75abd3](https://github.com/jbcom/radioactive-ralph/commit/a75abd3d60c1e9fb1eedd7ca859138f8438b7dcd))

## [0.10.2](https://github.com/jbcom/radioactive-ralph/compare/v0.10.1...v0.10.2) (2026-07-17)


### Bug Fixes

* resolve all 14 findings from the second-pass audit of the fix code ([#79](https://github.com/jbcom/radioactive-ralph/issues/79)) ([f8b3de2](https://github.com/jbcom/radioactive-ralph/commit/f8b3de2f4cec971010b8ea398f12b7ccb3c617c3))

## [0.10.1](https://github.com/jbcom/radioactive-ralph/compare/v0.10.0...v0.10.1) (2026-07-17)


### Bug Fixes

* resolve all 29 findings from the post-release multi-lens audit ([#76](https://github.com/jbcom/radioactive-ralph/issues/76)) ([e8268db](https://github.com/jbcom/radioactive-ralph/commit/e8268db5aa37bb95dbdfe76fef03471a4fd17486))

## [0.10.0](https://github.com/jbcom/radioactive-ralph/compare/v0.9.1...v0.10.0) (2026-07-17)


### Features

* rebuild as a supervised-execution runtime (supervisor architecture) ([#73](https://github.com/jbcom/radioactive-ralph/issues/73)) ([00c788d](https://github.com/jbcom/radioactive-ralph/commit/00c788d397494a45f16fdd21aeb888314a46d407))

## [0.9.1](https://github.com/jbcom/radioactive-ralph/compare/v0.9.0...v0.9.1) (2026-07-16)


### Bug Fixes

* **cassette:** data race on recorder start time ([#70](https://github.com/jbcom/radioactive-ralph/issues/70)) ([ab8fa9f](https://github.com/jbcom/radioactive-ralph/commit/ab8fa9fab24015aea781398fcabd2db7bba6f04b))

## [0.9.0](https://github.com/jbcom/radioactive-ralph/compare/v0.8.3...v0.9.0) (2026-07-16)


### Features

* **provider:** remove gemini as a shipped provider (deprecated 2026-06-18) ([#66](https://github.com/jbcom/radioactive-ralph/issues/66)) ([26a433e](https://github.com/jbcom/radioactive-ralph/commit/26a433e423a5d90c946f92b18fbdc326ab9bfa32))

## [0.8.3](https://github.com/jbcom/radioactive-ralph/compare/v0.8.2...v0.8.3) (2026-07-16)


### Bug Fixes

* close 4 critical durable-runtime safety gaps ([#63](https://github.com/jbcom/radioactive-ralph/issues/63)) ([c0f48b2](https://github.com/jbcom/radioactive-ralph/commit/c0f48b26ab7551668bf2af29e6649e9728a547be))

## [0.8.2](https://github.com/jbcom/radioactive-ralph/compare/v0.8.1...v0.8.2) (2026-04-16)


### Bug Fixes

* **docs:** use explicit-URL `brew tap` form ([#44](https://github.com/jbcom/radioactive-ralph/issues/44)) ([2e39c5e](https://github.com/jbcom/radioactive-ralph/commit/2e39c5ed3896b7ecab31d754031b424ef2f56a70))

## [0.8.1](https://github.com/jbcom/radioactive-ralph/compare/v0.8.0...v0.8.1) (2026-04-16)


### Bug Fixes

* **release:** goreleaser opens PRs on jbcom/pkgs instead of direct push ([#42](https://github.com/jbcom/radioactive-ralph/issues/42)) ([d7233b6](https://github.com/jbcom/radioactive-ralph/commit/d7233b6f70f31328b5b59554900027fa38b42292))

## [0.8.0](https://github.com/jbcom/radioactive-ralph/compare/v0.7.0...v0.8.0) (2026-04-16)


### Features

* ship repo service runtime ([#40](https://github.com/jbcom/radioactive-ralph/issues/40)) ([ba538cd](https://github.com/jbcom/radioactive-ralph/commit/ba538cd61e8cca21db66bbfd9cedef46c261d94d))

## [0.7.0](https://github.com/jbcom/radioactive-ralph/compare/v0.6.1...v0.7.0) (2026-04-15)


### Features

* omnibus — repo-service runtime polish, release fixes, docs ([#36](https://github.com/jbcom/radioactive-ralph/issues/36)) ([30db744](https://github.com/jbcom/radioactive-ralph/commit/30db744515c325e836c983186284a048234eeb03))

## [0.6.1](https://github.com/jbcom/radioactive-ralph/compare/v0.6.0...v0.6.1) (2026-04-15)


### Bug Fixes

* **ci:** use GitHub native auto-merge for bots ([#35](https://github.com/jbcom/radioactive-ralph/issues/35)) ([2722c20](https://github.com/jbcom/radioactive-ralph/commit/2722c20fa703ff38c6630f1f0da1af31fdbd758f))
* **release:** collapse to jbcom/pkgs + single CI_GITHUB_TOKEN ([#33](https://github.com/jbcom/radioactive-ralph/issues/33)) ([a086067](https://github.com/jbcom/radioactive-ralph/commit/a086067ced6145ad0aecbe6f39fdd8d0b590f8fd))

## [0.6.0](https://github.com/jbcom/radioactive-ralph/compare/v0.5.1...v0.6.0) (2026-04-15)


### Features

* M2 + M3 — Go rewrite, plandag, MCP, Starlight docs, packaging ([#32](https://github.com/jbcom/radioactive-ralph/issues/32)) ([4ed2819](https://github.com/jbcom/radioactive-ralph/commit/4ed28196b671e3fa092e0a3126185b6e02baedd5))
* **m2:** doctor + voice layers ([#30](https://github.com/jbcom/radioactive-ralph/issues/30)) ([5989700](https://github.com/jbcom/radioactive-ralph/commit/5989700012eea04cb3fc1fea7ed88aa90aa25d9c))
* **m2:** foundation — Go rewrite Python→reference + xdg/config/inventory/db layers ([#27](https://github.com/jbcom/radioactive-ralph/issues/27)) ([325f095](https://github.com/jbcom/radioactive-ralph/commit/325f095be532aa96c7d0bab72ac4be17321b9d5b))
* **m2:** multiplexer + ipc layers ([#29](https://github.com/jbcom/radioactive-ralph/issues/29)) ([51d90a7](https://github.com/jbcom/radioactive-ralph/commit/51d90a7beb65b32dd5add7d9fc1adefab5443bc6))

## Historical Appendix — M2 Rewrite Planning Snapshot

### Added — M2 rewrite (PR #31)

- **`internal/variant`** — all ten `Profile` definitions (blue, grey, green,
  red, professor, fixit, immortal, savage, old-man, world-breaker) with
  safety-floor enforcement. New `ShellExplicitlyTrusted` field catches the
  shared+Bash defense-in-depth hole Amazon Q flagged: Bash can run
  `git commit` and arbitrary subprocesses, so shared-isolation variants
  must explicitly opt into it.
- **`internal/workspace`** — mirror clone with `--reference`, worktree pool
  with monotonic branch naming, LFS per-mode config, hook copy preserving
  executable bit. Four orthogonal knobs (isolation/object_store/sync/LFS).
- **`internal/provider/claudesession`** — ClaudeSession wrapping `claude -p
  --input-format stream-json`. Session-ID pinning, sentinel re-prompt on
  resume, PromptRenderer combining variant biases with inventory-selected
  skills. Three test tiers: fake-claude unit tests, cassette-replay
  (deterministic VCR, no auth), gated live tests.
- **`internal/provider/claudesession/cassette`** — subprocess-level VCR. Recorder wraps
  a real claude subprocess and tees stdin+stdout to a JSON cassette;
  replayer binary replays the cassette in CI without credentials.
- **`internal/service`** — launchd + systemd-user unit installers with
  safety gates. Refuses `RefuseServiceContext` variants; requires explicit
  `GateConfirmed` for gated variants.
- **`internal/supervisor`** — PID flock, event replay, IPC dispatch,
  graceful shutdown. Integrates db + ipc + workspace.
- **`internal/initcmd`** — `radioactive_ralph init` capability wizard. Scaffolds
  `.radioactive-ralph/{config,local,plans/index.md}`, idempotent
  `.gitignore` updates, `--refresh` preserves operator choices.
- **`cmd/ralph`** — full Kong CLI (init/run/status/attach/stop/doctor/
  service + hidden `_supervisor`) with signal handling, plans-first
  discipline enforcement, and claude-binary pre-check.
- **`tests/integration`** — always-on end-to-end CLI harness plus gated
  live tests (`CLAUDE_AUTHENTICATED=1`).

### Changed

- `joe-fixit-ralph` renamed to `fixit-ralph` across code, skills, docs,
  and marketplace. Fixit is now the sole variant permitted to recommend
  peers via advisor mode; every other variant refuses to run without a
  valid `.radioactive-ralph/plans/index.md`.
- Task-batch PreCompact hook redirected from `$REPO/.claude/state/` to
  `$XDG_STATE_HOME/claude-code/task-batch` (Linux) or
  `~/Library/Application Support/claude-code/task-batch` (macOS) to
  respect the project rule that state lives outside the repo.
- **Architectural pivot** — the daemon is being rewritten into a per-repo
  meta-orchestrator that owns managed `claude -p` subprocesses via stream-json
  stdin/stdout. Rationale and full plan in
  [`docs/plans/2026-04-14-radioactive-ralph-rewrite.prq.md`](docs/plans/2026-04-14-radioactive-ralph-rewrite.prq.md).
- `.claude-plugin/marketplace.json` — marketplace renamed to `jbcom-plugins`,
  plugin renamed to `ralph`, `strict: false`, skills listed explicitly. The
  previous name collision (`radioactive-ralph@radioactive-ralph`) made the
  install invocation ambiguous; the new invocation is
  `claude plugin install ralph@jbcom-plugins`.
- README install + command documentation corrected to match the real CLI.
  The four phantom commands (`dashboard`, `discover`, `pr list/merge`,
  `install-skill`) and the `claude --print subprocesses` fiction are gone.
- Auth helpers moved from `github_client.py` to `forge/auth.py` where they
  properly belong alongside the `forge/github.py` that uses them.

### Removed

- `src/radioactive_ralph/github_client.py` (the legacy `GitHubClient` class).
  Dead code — `forge/github.py` was already the real implementation.
- `.claude-plugin/plugin.json` — redundant with `strict: false` marketplace entry.

### Deprecated / stubbed pending rewrite

- `Orchestrator.run()` and `Orchestrator.stop()` raise `NotImplementedError`
  with a pointer to the PRD. Inner helpers (`_merge_ready`, `_review_pending`,
  `_should_discover`) preserved as reusable building blocks for M2.
- `agent_runner.run_parallel_agents()` raises `NotImplementedError`. The
  previous implementation called `claude --message --yes`, which is not a
  real Claude CLI flag. Replacement lands in M2 (stream-json subprocess
  control).
- `radioactive_ralph run` CLI subcommand exits 2 with the rewrite pointer.

### Fixed

- `tests/test_cli.py::test_main_verbose` had an empty `pass` body; now
  asserts `--verbose` dispatches through `logging.basicConfig` with
  `DEBUG` level.
- `tests/test_orchestrator.py::test_step_spawns_agents` passed
  `repo_name` as a Pydantic kwarg where it's defined as a computed
  property; the test is removed (the underlying `_step` method is
  stubbed pending M2).

## [0.5.1](https://github.com/jbcom/radioactive-ralph/compare/v0.5.0...v0.5.1) (2026-04-10)


### Bug Fixes

* replace misleading comment in doctor.py health check function ([#22](https://github.com/jbcom/radioactive-ralph/issues/22)) ([774166b](https://github.com/jbcom/radioactive-ralph/commit/774166b75a169e04154413769cfe09b5ea321351))

## [0.5.0](https://github.com/jbcom/radioactive-ralph/compare/v0.4.0...v0.5.0) (2026-04-10)


### Features

* add automerge workflow and clean up CD ([#20](https://github.com/jbcom/radioactive-ralph/issues/20)) ([06bf01f](https://github.com/jbcom/radioactive-ralph/commit/06bf01ff277f27985eb751c5c73aed093db7824b))
* automate release asset generation ([#16](https://github.com/jbcom/radioactive-ralph/issues/16)) ([7474891](https://github.com/jbcom/radioactive-ralph/commit/74748912a30c59b4c6e5d7da3a5f51bdbde4abd3))


### Bug Fixes

* GitHub native PR comment for demo GIF ([#19](https://github.com/jbcom/radioactive-ralph/issues/19)) ([ba1fd89](https://github.com/jbcom/radioactive-ralph/commit/ba1fd89399b90c23e7b375d9e6d7239f6d0233e4))

## [0.4.0](https://github.com/jbcom/radioactive-ralph/compare/v0.3.0...v0.4.0) (2026-04-10)


### Features

* modernize documentation (Shibuya + AutoAPI + Fuzzy Bubbles) ([#12](https://github.com/jbcom/radioactive-ralph/issues/12)) ([2537f08](https://github.com/jbcom/radioactive-ralph/commit/2537f081343f3ca0900d77fb9733140b757afd5d))

## [0.3.0](https://github.com/jbcom/radioactive-ralph/compare/v0.2.0...v0.3.0) (2026-04-10)


### Features

* integrate SonarQube and test reporting ([#11](https://github.com/jbcom/radioactive-ralph/issues/11)) ([236b7a7](https://github.com/jbcom/radioactive-ralph/commit/236b7a7c82ea33a47cc96c4855d65006a1c55882))

## [0.2.0](https://github.com/jbcom/radioactive-ralph/compare/v0.1.0...v0.2.0) (2026-04-10)


### Features

* 100% test coverage and modernized CI/CD workflows ([#9](https://github.com/jbcom/radioactive-ralph/issues/9)) ([9f79067](https://github.com/jbcom/radioactive-ralph/commit/9f790671fc077be18b98bb9d3d4c7f5f832cc9c9))

## 0.1.0 (2026-04-10)


### Features

* forge abstraction layer + GitPython local git ops ([#3](https://github.com/jbcom/radioactive-ralph/issues/3)) ([9bcb26f](https://github.com/jbcom/radioactive-ralph/commit/9bcb26f86229c9afca26f2e460564643105f9c2a))
* initial radioactive-ralph v0.1.0 release ([b0af5c6](https://github.com/jbcom/radioactive-ralph/commit/b0af5c65b2aba3de7fae80194500081bf0c7e92c))

## Historical Appendix — Initial Python-Era Unreleased Snapshot

### Added
- Initial Python package structure with hatchling build
- `Orchestrator` async daemon loop with 8-phase cycle
- `PRManager` — gh CLI wrapper for PR classification and merge
- `Reviewer` — internal code review via Anthropic API (haiku/sonnet tiering)
- `WorkDiscovery` — scans repos for missing docs, reads STATE.md and DESIGN.md
- `AgentRunner` — spawns claude CLI subprocesses with model selection
- `State` — durable JSON persistence with dedup and pruning
- `AutoloopConfig` — TOML-based config with sensible defaults
- Click CLI: `radioactive_ralph run`, `radioactive_ralph status`, `ralph discover`, `ralph pr list/merge`, `radioactive_ralph stop`
- `uvx radioactive-ralph` support for zero-install execution
- Sphinx documentation with RTD theme, published to GitHub Pages
- CI/CD: GitHub Actions with OIDC PyPI publishing and Sphinx Pages deploy
- release-please for automated changelog and versioning
- dependabot for weekly dependency updates
