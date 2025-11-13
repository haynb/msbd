use std::path::PathBuf;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let proto_dir = PathBuf::from("../../../services/realtime-interview-core/proto/realtime/v1");
    tonic_build::configure().build_server(false).compile(
        &[
            proto_dir.join("streaming.proto"),
            proto_dir.join("control.proto"),
        ],
        &[proto_dir],
    )?;
    Ok(())
}
