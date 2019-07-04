FROM alpine:3.8

COPY ./rook-cephfs-provisioner /bin/rook-cephfs-provisioner

ENTRYPOINT [ "/bin/rook-cephfs-provisioner" ]