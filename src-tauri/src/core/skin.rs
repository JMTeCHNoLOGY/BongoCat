use sha2::{Digest, Sha256};
use std::{
    fs,
    path::{Path, PathBuf},
};
use tauri::command;

const IGNORED_FILES: &[&str] = &[".DS_Store", "Thumbs.db"];

#[command]
pub async fn compute_skin_id(path: String) -> Result<String, String> {
    tauri::async_runtime::spawn_blocking(move || hash_skin_directory(Path::new(&path)))
        .await
        .map_err(|error| error.to_string())?
}

pub fn hash_skin_directory(root: &Path) -> Result<String, String> {
    if !root.is_dir() {
        return Err("skin path is not a directory".into());
    }

    let mut files = Vec::new();
    collect_files(root, root, &mut files)?;
    files.sort();

    if files.is_empty() {
        return Err("skin directory is empty".into());
    }

    let mut digest = Sha256::new();
    for relative in files {
        let relative_text = relative.to_string_lossy().replace('\\', "/");
        let contents = fs::read(root.join(&relative))
            .map_err(|error| format!("failed to read {}: {error}", relative_text))?;
        let file_hash = Sha256::digest(contents);

        digest.update(relative_text.as_bytes());
        digest.update([0]);
        digest.update(file_hash);
    }

    Ok(format!("sha256:{:x}", digest.finalize()))
}

fn collect_files(root: &Path, current: &Path, files: &mut Vec<PathBuf>) -> Result<(), String> {
    let entries = fs::read_dir(current)
        .map_err(|error| format!("failed to read {}: {error}", current.display()))?;

    for entry in entries {
        let entry = entry.map_err(|error| error.to_string())?;
        let file_type = entry.file_type().map_err(|error| error.to_string())?;
        let name = entry.file_name();
        let name = name.to_string_lossy();

        if IGNORED_FILES.contains(&name.as_ref()) {
            continue;
        }

        let path = entry.path();
        if file_type.is_dir() {
            collect_files(root, &path, files)?;
        } else if file_type.is_file() {
            let relative = path.strip_prefix(root).map_err(|error| error.to_string())?;
            files.push(relative.to_path_buf());
        }
    }

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::{env, fs};

    #[test]
    fn skin_hash_is_stable_and_ignores_system_files() {
        let root = env::temp_dir().join(format!("bongocat-skin-test-{}", std::process::id()));
        let nested = root.join("resources");
        fs::create_dir_all(&nested).unwrap();
        fs::write(root.join("cat.model3.json"), b"model").unwrap();
        fs::write(nested.join("key.png"), b"image").unwrap();

        let first = hash_skin_directory(&root).unwrap();
        fs::write(root.join(".DS_Store"), b"ignored").unwrap();
        let second = hash_skin_directory(&root).unwrap();
        assert_eq!(first, second);

        fs::write(nested.join("key.png"), b"changed").unwrap();
        let third = hash_skin_directory(&root).unwrap();
        assert_ne!(first, third);

        fs::remove_dir_all(root).unwrap();
    }
}
