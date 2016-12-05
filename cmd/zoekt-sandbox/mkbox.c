/* mkbox.c
 *
 * Copyright 2014 Brian Swetland <swetland@frotz.net>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

#define _GNU_SOURCE

#include <errno.h>
#include <fcntl.h>
#include <linux/capability.h>
#include <sched.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mount.h>
#include <sys/prctl.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <unistd.h>


/* can't find headers for these, but they're in glibc... */
int pivot_root(const char *new_root, const char *put_old);

/* provided by sys/capability.h (libcap-dev), but provided here for
   easy compilation. */
int capset(cap_user_header_t h, cap_user_data_t d);
int capset(cap_user_header_t h, cap_user_data_t d);

#define errorf(...) do { fprintf(stderr, __VA_ARGS__); exit(-1); } while (0)

static int checkreturn(int res, const char *name, char *arg, int line) {
        if (res >= 0)
                return res;
        fprintf(stderr, "mkbox.c:%d: error: %s(%s) failed: r=%d errno=%d (%s)\n",
                line, name, arg, res, errno, strerror(errno));
        exit(-1);
}

#define ok(fname, arg...) checkreturn(fname(arg), #fname, #arg, __LINE__)

int dropcaps(void) {
        struct __user_cap_header_struct header;
        struct __user_cap_data_struct data[_LINUX_CAPABILITY_U32S_3];
        header.version = _LINUX_CAPABILITY_VERSION_3;
        header.pid = 0;
        memset(data, 0, sizeof(data));
        return capset(&header, data);
}

const char* my_domain = "localdomain";
const char* my_host = "localhost";

void recursive_mkdir(const char* dir, int mode) {
        int end = 0;

        while (dir[end] != '\0') {
                char path[1024] = {};
                char *endp = strchrnul(dir + end + 1, '/');
                strncpy(path, dir, endp - dir);
                end = endp - dir;

                struct stat buf;
                if (lstat(path, &buf) >= 0 && (buf.st_mode & S_IFDIR) != 0) {
                        continue;
                }

                if (mkdir(path, mode) < 0) {
                        fprintf(stderr, "mkdir(%s): %d", path, errno);
                        exit(-1);
                }
        }
}


int main(int argc, char **argv) {
        uid_t uid = getuid();
        gid_t gid = getgid();
        const char* child_dir = NULL;
        const char* binary = NULL;
        int verbose = 1;

        /* Ask the kernel to kill us with SIGKILL if our parent dies.
         * this carries over to the process launched via execv().
         */
        ok(prctl, PR_SET_PDEATHSIG, SIGKILL);

        /* CLONE_NEWNET kills performance for short-lived processes,
         * see https://lkml.org/lkml/2014/8/28/656), but let's avoid
         * rogue processes contacting other hosts. */
        int unshare_flags = CLONE_NEWNS|CLONE_NEWUTS|CLONE_NEWPID|
                CLONE_NEWIPC|CLONE_NEWUSER|CLONE_NEWNET;
        ok(unshare, unshare_flags);
        ok(setdomainname, my_domain, strlen(my_domain));
        ok(sethostname, my_host, strlen(my_host));
        int root_set = 0;
        int opt;
        while ((opt = getopt(argc, argv, "+b:B:d:D:g:qr:s:t:u:Z")) != -1) {
                switch (opt) {
                case 'q':       /* quiet */
                        verbose = 0;
                        break;
                case 's': // sandbox root directory
                        /* ensure that changes to our mount namespace
                          do not "leak" to outside namespaces (what
                          mount --make-rprivate / does)
                         */
                        mount("none", "/", NULL, MS_REC|MS_PRIVATE, NULL);

                        /* mount the sandbox on top of itself in our
                         new namespace. It will become our root
                         filesystem */
                        ok(mount, optarg, optarg, NULL, MS_BIND|MS_NOSUID, NULL);

                        /* step inside the to-be-root-directory */
                        if (verbose) {
                                fprintf(stderr, "root dir: %s\n", optarg);
                        }
                        ok(chdir, optarg);
                        root_set = 1;
                        break;

                case 'B':       /* binary to invoke */
                        binary = optarg;
                        break;

                case 'b': // bind mount directory or file
                {
                        char *dst = strchr(optarg, '=');
                        if (dst == NULL) {
                                errorf("argument must have '=': %s", optarg);
                        }
                        if (dst[1] == '/') {
                                errorf("destination for %s must be relative to sandbox root.\n", optarg);
                        }

                        *dst = '\0';
                        dst++;
                        char *src = optarg;

                        if (verbose) {
                                fprintf(stderr, "mount: %s => %s\n", src, dst);
                        }

                        struct stat buf = {};
                        ok(stat, src, &buf);

                        if (S_ISDIR(buf.st_mode)) {
                                 if (lstat(dst, &buf) < 0) {
                                         recursive_mkdir(dst, 0755);
                                 }

                                 /* must use MS_REC, otherwise can't
                                    bind-mount a directory that has
                                    other directories mounted below.

                                    The submounts won't be affected by
                                    MS_REMOUNT | MS_READONLY,
                                    unfortunately.
                                  */
                                ok(mount, src, dst, NULL, MS_REC|MS_BIND, NULL);
                        } else {
                                /* create bind points. Don't use
                                  O_EXCL so we can debug by repeatedly
                                  calling the same command-line. */
                                ok(close, ok(open, dst, O_WRONLY|O_CREAT, 0666));
                                ok(mount, src, dst, NULL, MS_BIND, NULL);
                        }
                }
                break;
                case 't': // setup tmp dir
                        if (verbose) {
                                fprintf(stderr, "tmp: %s\n", optarg);
                        }
                        struct stat buf = {};
                        if (lstat(optarg, &buf) < 0) {
                                recursive_mkdir(optarg, 0755);
                        }

                        ok(mount, "sandbox-tmp", optarg, "tmpfs",
                           MS_NOSUID|MS_NOEXEC|MS_NOATIME,
                           "size=16m,nr_inodes=16k,mode=755");
                        break;


                case 'u': // set UID
                        {
                                char buf[1024];
                                int newuid = -1;
                                if (sscanf(optarg, "%d", &newuid) != 1) {
                                        errorf("could not parse %s", optarg);
                                }

                                sprintf(buf, "%d %d 1\n", newuid, uid);
                                int fd = ok(open, "/proc/self/uid_map", O_WRONLY);
                                ok(write, fd, buf, strlen(buf));
                                ok(close, fd);
                                ok(setresuid, newuid, newuid, newuid);
                        }
                        break;

                case 'g': // set GID.
                        {
                                char buf[1024];
                                /* write "deny" to
                                   /proc/self/setgroups in order for
                                   our unprivileged process to be able
                                   to write arbitrary group IDs to
                                   gid_map.

                                   this proc file doesn't exist in
                                   older Linux kernels, in which case
                                   the correct fallback is to just
                                   ignore it (because that signals
                                   that the additional security check
                                   that /proc/self/setgroups relates
                                   to doesn't exist it).
                                */
                                int fd = open("/proc/self/setgroups", O_WRONLY);
                                if (fd > 0)  {
                                        strcpy(buf, "deny");
                                        ok(write, fd, buf, strlen(buf));
                                        ok(close, fd);
                                }

                                int newgid = -1;
                                if (sscanf(optarg, "%d", &newgid) != 1) {
                                        errorf("could not parse %s", optarg);
                                }

                                sprintf(buf, "%d %d 1\n", newgid, gid);
                                fd = ok(open, "/proc/self/gid_map", O_WRONLY);
                                ok(write, fd, buf, strlen(buf));
                                ok(close, fd);

                                /* initially we're nobody, change to new GID */
                                ok(setresgid, newgid, newgid, newgid);
                        }
                        break;

                case 'd': // dir for process
                        child_dir = optarg;
                        break;

                case 'D':
                        /* create dir. Needed for creating dirs inside
                           tmp/ , or bind mounts in subdirectories
                         */
                        recursive_mkdir(optarg, 0755);
                        break;

                default:
                        errorf("option %c unknown", opt);

                }
        }

        if (!root_set) {
                errorf("-s option is mandatory");
        }

        /* sandbox becomes our new root, detach the old one */
        ok(mkdir, ".oldroot", 0755);
        ok(pivot_root, ".", ".oldroot");

        /* pivot_root() may or may not affect its current working
         * directory.  It is therefore recommended to call chdir("/")
         * immediately after pivot_root(). */
        ok(chroot, ".");
        ok(umount2, ".oldroot", MNT_DETACH);
        ok(rmdir, ".oldroot");

        /* remount root to finalize permissions */
        ok(mount, "/", "/", NULL,
                MS_REMOUNT|MS_BIND|MS_NOEXEC|MS_NOSUID|MS_NODEV|MS_RDONLY,
                NULL);

        if (child_dir != NULL) {
                ok(chdir, child_dir);
        }

        ok(dropcaps);
        if (binary == NULL){
                binary = argv[optind];
        }
        ok(execv, binary, argv + optind);
}
