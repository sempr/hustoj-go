FROM rust:1-slim
RUN TOOLCHAIN=$(ls /usr/local/rustup/toolchains | head -n1) && \
    ln -sf /usr/local/rustup/toolchains/$TOOLCHAIN/bin/rustc /usr/local/bin/rustc

