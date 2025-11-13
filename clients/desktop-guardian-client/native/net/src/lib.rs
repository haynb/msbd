pub mod stream_client;

pub mod proto {
    pub mod realtime {
        pub mod v1 {
            include!(concat!(env!("OUT_DIR"), "/realtime.v1.rs"));
        }
    }
}
