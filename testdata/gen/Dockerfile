FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y \
    gcc \
    liblzo2-dev \
    && rm -rf /var/lib/apt/lists/*

COPY gen_vectors.c /src/gen_vectors.c
WORKDIR /src

RUN gcc -O2 -o gen_vectors gen_vectors.c -llzo2

CMD ["./gen_vectors"]
