# Storage backend benchmark (B13-T03)

Command: `VSF_MINIO=artifacts/tools/minio vsfstoragebench -tiny 200000 -versions 200 -version-size 524288 -reads 50000 -mixed 15s -crash-runs 5 -crash-objects 20000 -out storage-benchmark.json`. Raw results: `storage-benchmark.json`; the comparison table is `storage-benchmark.txt`.

## What was measured
Each backend ran in its own process over the same seeded corpus: 200,000 tiny objects (100 to 2000 bytes, 200 MiB), 200 similar large versions (512 KiB each, about 2% of bytes changed from the previous one, 100 MiB), 50,000 random tiny reads, 15 s of 70% read / 30% write with 8 workers, deletion of half the tiny objects followed by the backend's own maintenance, a restart, and five kill -9 runs in which the writer was killed with acknowledged writes in flight.

## Backends
- **filesystem**: `blob.FS`, one file per object, temp file, fsync, rename, directory fsync.
- **sqlite**: `blob.SQLite`, one WAL database, `synchronous=FULL`, one transaction per object.
- **s3-minio**: `blob.S3` (SigV4, `If-None-Match: *`) against a MinIO server built from `github.com/minio/minio v0.0.0-20260212201848-7aac2a2c5b7c` (`go install`, run as a separate process; AGPL, used only as a test server and neither linked nor shipped), loopback, one drive.
- **ZEROS3**: not measured. The repository holds no endpoint, credentials or client for it. If it exposes an S3 dialect, `blob.S3` can run against it unchanged.

## Limits of these numbers
- One 4-core virtual machine, ext4 on a virtual disk, one run per backend. Latency and throughput will differ on other hardware; the ratios are the useful part.
- 200,000 tiny objects, not the 1,000,000 of the plan; a million would have taken over an hour per backend here.
- The similar-version corpus is synthetic (eight scattered 0.25% runs per version). How similar real compiled artifacts are is not known.
- Peak RSS for the in-process backends includes Go heap growth from the harness's read and write buffers and is not a clean backend figure; MinIO's is its own process.
- MinIO removes deleted objects in the background, so its size after deletion had not settled when it was measured (918 to 841 MiB while about 390 MiB were removed); its 358 s delete time is the client-visible time.
- Device writes come from `/proc/<pid>/io` of the backend's process (the writer child for filesystem and SQLite, the server for MinIO).
