FROM alpine:3.20
RUN apk add --no-cache openssh-server openssh-client findutils coreutils \
 && adduser -D -s /bin/sh tester \
 && echo "tester:secret" | chpasswd \
 && mkdir -p /home/tester/.ssh && chmod 700 /home/tester/.ssh && chown tester:tester /home/tester/.ssh \
 && sed -i -E '/^#?(PasswordAuthentication|KbdInteractiveAuthentication|AllowTcpForwarding|PermitRootLogin)[[:space:]]/d' /etc/ssh/sshd_config \
 && printf 'PasswordAuthentication yes\nKbdInteractiveAuthentication yes\nAllowTcpForwarding yes\nPermitRootLogin no\n' >> /etc/ssh/sshd_config
EXPOSE 22
# Host keys are generated per container so every server has its own identity.
CMD ["/bin/sh", "-c", "ssh-keygen -A >/dev/null && echo \"$AUTHORIZED_KEY\" > /home/tester/.ssh/authorized_keys && chown tester:tester /home/tester/.ssh/authorized_keys && chmod 600 /home/tester/.ssh/authorized_keys && exec /usr/sbin/sshd -D -e"]
