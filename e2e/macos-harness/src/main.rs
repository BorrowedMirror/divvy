//! macOS e2e harness (works on any OS). WebDriver is unavailable for WKWebView, so this
//! drives the app purely over its debug test hook:
//!
//! 1. serve a mock trade2 API (`kalandra_testkit::fixtures::TradeMock`) on localhost,
//! 2. start `fake-game` with an item and wait for `READY`,
//! 3. start kalandra with `KALANDRA_TEST_HOOK`, `KALANDRA_TRADE_BASE_URL`, `KALANDRA_CONFIG_DIR`,
//! 4. `trigger 0`, wait for the game's `COPIED 1`, poll `state` until the overlay shows a price,
//! 5. assert the median, that search + fetch hit the mock, then `quit`.
//!
//! Env: `KALANDRA_APP_BIN`, `KALANDRA_FAKE_GAME_BIN`, `KALANDRA_TEST_HOOK` (port, default 47123),
//! `KALANDRA_E2E_ITEM`, `KALANDRA_E2E_TIMEOUT_SECS` (default 30).
use std::io::{BufRead, BufReader, Write};
use std::net::TcpStream;
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::sync::mpsc::{Receiver, RecvTimeoutError, channel};
use std::time::{Duration, Instant};

use anyhow::{Context, bail, ensure};
use kalandra_testkit::fixtures::TradeMock;
use serde_json::Value;

const EXE: &str = std::env::consts::EXE_SUFFIX;

fn repo() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../..")
}

fn env_path(key: &str, default: PathBuf) -> PathBuf {
    std::env::var_os(key).map_or(default, PathBuf::from)
}

/// Kills the child on drop so a failed assertion never leaks windows.
struct Guard(Child);

impl Drop for Guard {
    fn drop(&mut self) {
        if matches!(self.0.try_wait(), Ok(None)) {
            let _ = self.0.kill();
            let _ = self.0.wait();
        }
    }
}

fn spawn_fake_game(bin: &Path, item: &Path) -> anyhow::Result<(Guard, Receiver<String>)> {
    let mut child = Command::new(bin)
        .args(["--exit-after", "300", "--item"])
        .arg(item)
        .stdout(Stdio::piped())
        .spawn()
        .with_context(|| format!("spawning {}", bin.display()))?;
    let stdout = child.stdout.take().context("fake-game stdout")?;
    let (tx, rx) = channel();
    std::thread::spawn(move || {
        for line in BufReader::new(stdout).lines().map_while(Result::ok) {
            if tx.send(line).is_err() {
                break;
            }
        }
    });
    Ok((Guard(child), rx))
}

fn wait_line(rx: &Receiver<String>, want: &str, timeout: Duration) -> anyhow::Result<()> {
    let deadline = Instant::now() + timeout;
    loop {
        let left = deadline.saturating_duration_since(Instant::now());
        match rx.recv_timeout(left) {
            Ok(l) if l == want => return Ok(()),
            Ok(_) => {}
            Err(RecvTimeoutError::Timeout) => bail!("fake-game did not print `{want}` within {timeout:?}"),
            Err(RecvTimeoutError::Disconnected) => bail!("fake-game exited before printing `{want}`"),
        }
    }
}

struct Hook {
    port: u16,
}

impl Hook {
    fn command(&self, cmd: &str) -> anyhow::Result<String> {
        let mut s = TcpStream::connect(("127.0.0.1", self.port))
            .with_context(|| format!("connecting to test hook on 127.0.0.1:{}", self.port))?;
        s.set_read_timeout(Some(Duration::from_secs(5)))?;
        writeln!(s, "{cmd}")?;
        let mut line = String::new();
        BufReader::new(s).read_line(&mut line).with_context(|| format!("reading reply to `{cmd}`"))?;
        Ok(line.trim().to_owned())
    }

    fn ok(&self, cmd: &str) -> anyhow::Result<()> {
        let r = self.command(cmd)?;
        ensure!(r == "ok", "test hook `{cmd}` replied `{r}`");
        Ok(())
    }

    fn state(&self) -> anyhow::Result<Value> {
        let r = self.command("state")?;
        serde_json::from_str(&r).with_context(|| format!("`state` reply is not JSON: {r}"))
    }

    fn wait_up(&self, app: &mut Child, timeout: Duration) -> anyhow::Result<()> {
        let deadline = Instant::now() + timeout;
        loop {
            if let Some(status) = app.try_wait()? {
                bail!("kalandra exited ({status}) before the test hook came up");
            }
            match self.state() {
                Ok(_) => return Ok(()),
                Err(e) if Instant::now() >= deadline => {
                    return Err(e.context(format!(
                        "test hook not reachable after {timeout:?}; is the app built with \
                         `--features test-hook` and does it honour KALANDRA_TEST_HOOK?"
                    )));
                }
                Err(_) => std::thread::sleep(Duration::from_millis(250)),
            }
        }
    }
}

fn median(state: &Value) -> Option<(f64, String)> {
    let m = state.get("last_price")?.get("median")?;
    Some((m.get("amount")?.as_f64()?, m.get("currency")?.as_str()?.to_owned()))
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    match run().await {
        Ok(()) => {
            println!("PASS: price check end-to-end over the test hook");
            Ok(())
        }
        Err(e) => {
            eprintln!("FAIL: {e:#}");
            std::process::exit(1);
        }
    }
}

async fn run() -> anyhow::Result<()> {
    let root = repo();
    let app_bin = env_path("KALANDRA_APP_BIN", root.join(format!("target/debug/kalandra{EXE}")));
    let game_bin = env_path("KALANDRA_FAKE_GAME_BIN", root.join(format!("target/debug/fake-game{EXE}")));
    let item = env_path("KALANDRA_E2E_ITEM", root.join("e2e/fixtures/rare-ring.txt"));
    let port: u16 =
        std::env::var("KALANDRA_TEST_HOOK").ok().map_or(Ok(47123), |p| p.parse()).context("KALANDRA_TEST_HOOK")?;
    let timeout =
        Duration::from_secs(std::env::var("KALANDRA_E2E_TIMEOUT_SECS").ok().and_then(|s| s.parse().ok()).unwrap_or(30));
    for (bin, hint) in [
        (&app_bin, "cargo build -p kalandra-desktop --features test-hook,tauri/custom-protocol"),
        (&game_bin, "cargo build -p fake-game"),
    ] {
        ensure!(bin.is_file(), "{} not found; build it with `{hint}` (or set the env override)", bin.display());
    }
    ensure!(item.is_file(), "item file {} not found", item.display());

    let server = TradeMock::new()
        .leagues(&["Standard"])
        .prices(&[(1.0, "divine"), (2.0, "divine"), (3.0, "divine")])
        .serve("127.0.0.1:0")
        .await?;
    println!("mock trade2 API at {}", server.uri());

    let (_game, game_out) = spawn_fake_game(&game_bin, &item)?;
    wait_line(&game_out, "READY", Duration::from_secs(15))?;
    println!("fake-game ready");

    let config_dir = std::env::temp_dir().join(format!("kalandra-harness-{}", std::process::id()));
    std::fs::create_dir_all(&config_dir)?;
    let mut app = Guard(
        Command::new(&app_bin)
            .env("KALANDRA_TEST_HOOK", port.to_string())
            .env("KALANDRA_TRADE_BASE_URL", server.uri())
            .env("KALANDRA_CONFIG_DIR", &config_dir)
            .env("RUST_LOG", std::env::var("RUST_LOG").unwrap_or_else(|_| "kalandra=debug,info".into()))
            .spawn()
            .with_context(|| format!("spawning {}", app_bin.display()))?,
    );
    let hook = Hook { port };
    hook.wait_up(&mut app.0, timeout)?;
    println!("test hook up on {port}");

    let initial = hook.state()?;
    ensure!(initial["overlay_visible"] == Value::Bool(false), "overlay visible before any hotkey: {initial}");

    hook.ok("trigger 0")?;
    wait_line(&game_out, "COPIED 1", Duration::from_secs(10))
        .context("the app never sent the copy chord to the game window (focus? accessibility permission?)")?;

    let deadline = Instant::now() + timeout;
    let state = loop {
        let s = hook.state()?;
        if s["overlay_visible"] == Value::Bool(true) && !s["last_price"].is_null() {
            break s;
        }
        ensure!(Instant::now() < deadline, "no price in the overlay after {timeout:?}; last state: {s}");
        tokio::time::sleep(Duration::from_millis(200)).await;
    };
    let m = median(&state).with_context(|| format!("state has no median: {state}"))?;
    ensure!(m == (2.0, "divine".to_owned()), "median {m:?} != 2 divine; state: {state}");

    let reqs = server.received_requests().await.unwrap_or_default();
    let hit = |p: &str| reqs.iter().any(|r| r.url.path().starts_with(p));
    ensure!(hit("/api/trade2/search/poe2/"), "app never called trade2 search");
    ensure!(hit("/api/trade2/fetch/"), "app never called trade2 fetch");

    hook.ok("quit")?;
    let deadline = Instant::now() + Duration::from_secs(10);
    while app.0.try_wait()?.is_none() {
        ensure!(Instant::now() < deadline, "app still running 10s after `quit`");
        std::thread::sleep(Duration::from_millis(100));
    }
    let _ = std::fs::remove_dir_all(&config_dir);
    Ok(())
}
