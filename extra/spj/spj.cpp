#include <stdio.h>

#define AC 0
#define WA 1
int main(int argc,char *args[])
{
    FILE * f_in = fopen(args[1],"r");
    FILE * f_out = fopen(args[2],"r");
    FILE * f_user = fopen(args[3],"r");
    int ret = AC;
    int t;
    int x,y,z;
    fscanf(f_in, "%d", &t);
    while (t--) {
        int cn=fscanf(f_user, "%d%d", &y,&z);
        if (cn!=2){
          ret=WA;
          break;
        }
        fscanf(f_in, "%d", &x);
        if (y>0&&z>0&&y+z==x) continue;
        ret=WA;
        break;
    }
    fclose(f_in);
    fclose(f_out);
    fclose(f_user);
    fprintf(stderr,"result=%d", ret);
    return ret;
}

