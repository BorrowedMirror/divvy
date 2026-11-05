fn main() {
    // `tauri::generate_context!` requires `frontendDist` to exist. Create a placeholder so
    // Rust-only builds (CI, tests) work without building the web app first.
    let dist = std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join("../../../web/dist");
    if !dist.join("index.html").exists() {
        let _ = std::fs::create_dir_all(&dist);
        let _ = std::fs::write(dist.join("index.html"), "<!doctype html><title>kalandra</title>");
    }
    tauri_build::build();
}
