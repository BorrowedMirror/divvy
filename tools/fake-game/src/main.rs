//! fake-game: opens a window titled "Path of Exile 2". On Ctrl+C / Ctrl+Alt+C it puts the
//! chosen corpus item (normal / advanced variant) on the clipboard, like the real game does.
//!
//! Stdout protocol (line-buffered, flushed): `READY` once the window has been drawn and
//! mapped, `COPIED <n>` after the n-th copy (n starts at 1), `EXIT` before exiting.
//!
//! The clipboard handle stays alive for the whole run: on X11 the selection owner must keep
//! serving the data, so dropping it would empty the clipboard.
use std::io::Write as _;
use std::num::NonZeroU32;
use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::time::{Duration, Instant};

use anyhow::{Context as _, bail};
use clap::Parser;
use winit::application::ApplicationHandler;
use winit::event::{ElementState, KeyEvent, WindowEvent};
use winit::event_loop::{ActiveEventLoop, ControlFlow, EventLoop};
use winit::keyboard::{Key, KeyCode, ModifiersState, PhysicalKey};
use winit::window::{Window, WindowId};

const TITLE: &str = "Path of Exile 2";

#[derive(Debug, Parser)]
#[command(about = "Fake Path of Exile 2 window for rkl e2e tests")]
struct Args {
    /// Item text file served on Ctrl+C.
    #[arg(long, conflicts_with = "corpus_id")]
    item: Option<PathBuf>,
    /// Item text file served on Ctrl+Alt+C (defaults to the normal item).
    #[arg(long, conflicts_with = "advanced_corpus_id")]
    advanced_item: Option<PathBuf>,
    /// Corpus id (e.g. `currency/divine-orb`) from crates/testkit/corpus, served on Ctrl+C.
    #[arg(long)]
    corpus_id: Option<String>,
    /// Corpus id served on Ctrl+Alt+C.
    #[arg(long)]
    advanced_corpus_id: Option<String>,
    /// Corpus directory (defaults to the in-repo crates/testkit/corpus).
    #[arg(long, env = "RKL_CORPUS_DIR")]
    corpus_dir: Option<PathBuf>,
    /// Exit after this many seconds (CI safety net).
    #[arg(long)]
    exit_after: Option<f64>,
    /// Window size `WxH`.
    #[arg(long, default_value = "1280x720", value_parser = parse_size)]
    size: (u32, u32),
}

fn parse_size(s: &str) -> Result<(u32, u32), String> {
    let (w, h) = s.split_once('x').ok_or("expected WxH")?;
    let w: u32 = w.parse().map_err(|e| format!("width: {e}"))?;
    let h: u32 = h.parse().map_err(|e| format!("height: {e}"))?;
    if w == 0 || h == 0 {
        return Err("size must be non-zero".into());
    }
    Ok((w, h))
}

fn default_corpus_dir() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../../crates/testkit/corpus")
}

fn read_text(path: &Path) -> anyhow::Result<String> {
    std::fs::read_to_string(path).with_context(|| format!("reading item text {}", path.display()))
}

/// The two texts the game serves.
#[derive(Debug, Clone, PartialEq, Eq)]
struct Items {
    normal: String,
    advanced: String,
}

impl Items {
    fn from_args(a: &Args) -> anyhow::Result<Self> {
        let dir = a.corpus_dir.clone().unwrap_or_else(default_corpus_dir);
        let corpus = |id: &str| read_text(&dir.join(format!("{id}.txt")));
        let normal = match (&a.item, &a.corpus_id) {
            (Some(p), _) => read_text(p)?,
            (None, Some(id)) => corpus(id)?,
            (None, None) => bail!("one of --item or --corpus-id is required"),
        };
        let advanced = match (&a.advanced_item, &a.advanced_corpus_id) {
            (Some(p), _) => read_text(p)?,
            (None, Some(id)) => corpus(id)?,
            (None, None) => normal.clone(),
        };
        Ok(Self { normal, advanced })
    }

    /// Text to copy for a key press, if it is a copy chord. Cmd counts as Ctrl so the
    /// tool also behaves on macOS. The logical key is checked too: virtual keyboards
    /// (wtype) upload their own keymap, so the physical scancode is arbitrary there.
    fn for_key(&self, key: PhysicalKey, logical: &Key, mods: ModifiersState) -> Option<&str> {
        let is_c = key == PhysicalKey::Code(KeyCode::KeyC)
            || matches!(logical, Key::Character(s) if s.eq_ignore_ascii_case("c"));
        if !is_c || !(mods.control_key() || mods.super_key()) {
            return None;
        }
        Some(if mods.alt_key() { &self.advanced } else { &self.normal })
    }
}

fn say(line: &str) {
    let mut out = std::io::stdout().lock();
    let _ = writeln!(out, "{line}");
    let _ = out.flush();
}

struct Gfx {
    window: Arc<Window>,
    surface: softbuffer::Surface<Arc<Window>, Arc<Window>>,
}

struct App {
    items: Items,
    clipboard: arboard::Clipboard,
    deadline: Option<Instant>,
    size: (u32, u32),
    gfx: Option<Gfx>,
    mods: ModifiersState,
    copies: u64,
    ready: bool,
    error: Option<anyhow::Error>,
    /// A copy-chord press was seen and its release is still pending.
    c_down: bool,
    /// `FAKE_GAME_DEBUG=1`: log every window event to stderr.
    debug: bool,
}

impl App {
    fn create_window(&mut self, el: &ActiveEventLoop) -> anyhow::Result<()> {
        let attrs = Window::default_attributes()
            .with_title(TITLE)
            .with_inner_size(winit::dpi::LogicalSize::new(self.size.0, self.size.1));
        let window = Arc::new(el.create_window(attrs)?);
        let context = softbuffer::Context::new(window.clone()).map_err(|e| anyhow::anyhow!("softbuffer: {e}"))?;
        let surface =
            softbuffer::Surface::new(&context, window.clone()).map_err(|e| anyhow::anyhow!("softbuffer: {e}"))?;
        window.request_redraw();
        self.gfx = Some(Gfx { window, surface });
        Ok(())
    }

    fn draw(&mut self) -> anyhow::Result<()> {
        let Some(gfx) = self.gfx.as_mut() else { return Ok(()) };
        let size = gfx.window.inner_size();
        let (Some(w), Some(h)) = (NonZeroU32::new(size.width), NonZeroU32::new(size.height)) else {
            return Ok(());
        };
        gfx.surface.resize(w, h).map_err(|e| anyhow::anyhow!("resize: {e}"))?;
        let mut buf = gfx.surface.buffer_mut().map_err(|e| anyhow::anyhow!("buffer: {e}"))?;
        // dark brown, the colour of every PoE screenshot
        buf.fill(0x0020_1810 | (self.copies as u32 & 0xff) << 16);
        buf.present().map_err(|e| anyhow::anyhow!("present: {e}"))?;
        if !self.ready {
            self.ready = true;
            say("READY");
        }
        Ok(())
    }

    fn copy(&mut self, text: String) {
        match self.clipboard.set_text(text) {
            Ok(()) => {
                self.copies += 1;
                say(&format!("COPIED {}", self.copies));
            }
            Err(e) => eprintln!("fake-game: clipboard error: {e}"),
        }
        if let Some(g) = &self.gfx {
            g.window.request_redraw();
        }
    }

    fn fail(&mut self, el: &ActiveEventLoop, e: anyhow::Error) {
        self.error = Some(e);
        el.exit();
    }
}

impl ApplicationHandler for App {
    fn resumed(&mut self, el: &ActiveEventLoop) {
        if self.gfx.is_none()
            && let Err(e) = self.create_window(el)
        {
            self.fail(el, e);
        }
    }

    fn window_event(&mut self, el: &ActiveEventLoop, _id: WindowId, event: WindowEvent) {
        if self.debug {
            eprintln!("fake-game: {event:?}");
        }
        match event {
            WindowEvent::CloseRequested => el.exit(),
            WindowEvent::Resized(_) => {
                if let Some(g) = &self.gfx {
                    g.window.request_redraw();
                }
            }
            WindowEvent::RedrawRequested => {
                if let Err(e) = self.draw() {
                    self.fail(el, e);
                }
            }
            WindowEvent::ModifiersChanged(m) => self.mods = m.state(),
            WindowEvent::KeyboardInput {
                event: KeyEvent { physical_key, logical_key, state, repeat: false, .. },
                ..
            } => {
                let Some(text) = self.items.for_key(physical_key, &logical_key, self.mods) else { return };
                let text = text.to_owned();
                // Copy on press. Virtual keyboards (wtype) swap the keymap right before the
                // press, and winit can drop that press; then copy on the orphan release.
                match state {
                    ElementState::Pressed => {
                        self.c_down = true;
                        self.copy(text);
                    }
                    ElementState::Released if !self.c_down => self.copy(text),
                    ElementState::Released => self.c_down = false,
                }
            }
            _ => {}
        }
    }

    fn about_to_wait(&mut self, el: &ActiveEventLoop) {
        match self.deadline {
            Some(d) if Instant::now() >= d => el.exit(),
            Some(d) => el.set_control_flow(ControlFlow::WaitUntil(d)),
            None => el.set_control_flow(ControlFlow::Wait),
        }
    }
}

fn main() -> anyhow::Result<()> {
    let args = Args::parse();
    let items = Items::from_args(&args)?;
    let clipboard = arboard::Clipboard::new().context("opening clipboard")?;
    let event_loop = EventLoop::new().context("creating event loop (is DISPLAY / WAYLAND_DISPLAY set?)")?;
    let mut app = App {
        items,
        clipboard,
        deadline: args.exit_after.map(|s| Instant::now() + Duration::from_secs_f64(s.max(0.0))),
        size: args.size,
        gfx: None,
        mods: ModifiersState::empty(),
        copies: 0,
        ready: false,
        error: None,
        c_down: false,
        debug: std::env::var_os("FAKE_GAME_DEBUG").is_some_and(|v| v == "1"),
    };
    event_loop.run_app(&mut app)?;
    say("EXIT");
    app.error.map_or(Ok(()), Err)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn items() -> Items {
        Items { normal: "N".into(), advanced: "A".into() }
    }

    fn args(extra: &[&str]) -> Args {
        Args::try_parse_from(std::iter::once("fake-game").chain(extra.iter().copied())).unwrap()
    }

    #[test]
    fn chords() {
        let it = items();
        let c = PhysicalKey::Code(KeyCode::KeyC);
        let d = PhysicalKey::Code(KeyCode::KeyD);
        let lc = Key::Character("c".into());
        let ld = Key::Character("d".into());
        assert_eq!(it.for_key(c, &lc, ModifiersState::CONTROL), Some("N"));
        assert_eq!(it.for_key(c, &ld, ModifiersState::SUPER), Some("N"));
        assert_eq!(it.for_key(d, &Key::Character("C".into()), ModifiersState::CONTROL), Some("N"));
        assert_eq!(it.for_key(c, &lc, ModifiersState::CONTROL | ModifiersState::ALT), Some("A"));
        assert_eq!(it.for_key(c, &lc, ModifiersState::empty()), None);
        assert_eq!(it.for_key(c, &lc, ModifiersState::ALT), None);
        assert_eq!(it.for_key(d, &ld, ModifiersState::CONTROL), None);
    }

    #[test]
    fn corpus_id_resolves_and_advanced_defaults_to_normal() {
        let it = Items::from_args(&args(&["--corpus-id", "currency/divine-orb"])).unwrap();
        assert!(it.normal.starts_with("Item Class:"));
        assert_eq!(it.normal, it.advanced);
    }

    #[test]
    fn files_and_errors() {
        let dir = std::env::temp_dir().join(format!("fake-game-{}", std::process::id()));
        std::fs::create_dir_all(&dir).unwrap();
        std::fs::write(dir.join("n.txt"), "normal").unwrap();
        std::fs::write(dir.join("a.txt"), "adv").unwrap();
        let n = dir.join("n.txt").display().to_string();
        let a = dir.join("a.txt").display().to_string();
        let it = Items::from_args(&args(&["--item", &n, "--advanced-item", &a])).unwrap();
        assert_eq!(it, Items { normal: "normal".into(), advanced: "adv".into() });
        let it = Items::from_args(&args(&[
            "--item",
            &n,
            "--advanced-corpus-id",
            "a",
            "--corpus-dir",
            &dir.display().to_string(),
        ]))
        .unwrap();
        assert_eq!(it.advanced, "adv");
        assert!(Items::from_args(&args(&[])).is_err());
        assert!(Items::from_args(&args(&["--item", "/nonexistent/x.txt"])).is_err());
        assert!(Args::try_parse_from(["fake-game", "--item", "x", "--corpus-id", "y"]).is_err());
        std::fs::remove_dir_all(dir).unwrap();
    }

    #[test]
    fn sizes() {
        assert_eq!(parse_size("640x480"), Ok((640, 480)));
        assert!(parse_size("640").is_err());
        assert!(parse_size("0x5").is_err());
        assert!(parse_size("ax5").is_err());
        assert!(parse_size("5xb").is_err());
        assert_eq!(args(&["--exit-after", "1.5"]).exit_after, Some(1.5));
    }
}
