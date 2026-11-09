#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <pwd.h>
#include <grp.h>
#include <unistd.h>
#include <sys/types.h>
#include <sys/sysmacros.h>
 
static int allowed(const char *p) {
    if (strncmp(p, "/srv/hustoj/", 12) != 0 && strncmp(p, "/home/judge/", 12) != 0) return 0;
    char b[4096]; strcpy(b,p);
    char *s=strrchr(b,'/'); if(!s||strcmp(s+1,"null")) return 0;
    *s=0; char *d=strrchr(b,'/'); return d && !strcmp(d+1,"dev");
}
 
int main(int argc, char **argv) {
    if (argc != 2) { fprintf(stderr,"usage\n"); return 2; }
    size_t len=strlen(argv[1]); if (len==0||len>=4096) return 1;
    char path[4096]; snprintf(path,sizeof(path),"%s",argv[1]);
    if (!allowed(path)) { fprintf(stderr,"path not allowed\n"); return 1; }
    char parent[4096]; snprintf(parent,sizeof(parent),"%s",path);
    char *s=strrchr(parent,'/'); *s=0;
    // recursively mkdir parent
    char tmp[4096]; snprintf(tmp,sizeof(tmp),"%s",parent);
    for (int i=1; tmp[i]; ++i) {
        if (tmp[i]=='/') {
            char sv=tmp[i]; tmp[i]=0; mkdir(tmp,0755); tmp[i]=sv;
        }
    }
    mkdir(parent,0755);
    struct stat st;
    if (lstat(path,&st)==0) {
        if (S_ISCHR(st.st_mode)) { return 0; }
        unlink(path);
    }
    if (mknod(path, S_IFCHR|0666, makedev(1,3))!=0) { perror("mknod"); return 1; }
    chmod(path,0666);
    struct passwd *pw=getpwnam("nobody");
    struct group *gr=getgrnam("judge");
    if (pw&&gr) chown(path,gr->gr_gid?0:0,gr->gr_gid);
    // set owner root:judge
    return 0;
}
