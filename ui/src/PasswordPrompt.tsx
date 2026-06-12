import React from 'react';
import {Button, FormControl, Grid, Link, Paper, TextField, Typography} from '@mui/material';

export interface PasswordRequired {
    id: string;
    rejected: boolean;
}

export const PasswordPrompt = ({
    info,
    onSubmit,
}: {
    info: PasswordRequired;
    onSubmit: (password: string) => void;
}) => {
    const [password, setPassword] = React.useState('');
    const submit = (e: React.FormEvent) => {
        e.preventDefault();
        onSubmit(password);
    };
    return (
        <Grid
            container={true}
            sx={{justifyContent: 'center'}}
            style={{paddingTop: 50, maxWidth: 400, width: '100%', margin: '0 auto'}}
            spacing={4}
        >
            <Grid size={12}>
                <Typography align="center" gutterBottom>
                    <img src="./logo.svg" style={{width: 230}} alt="logo" />
                </Typography>
                <Paper elevation={3} style={{padding: 20}}>
                    <form onSubmit={submit}>
                        <FormControl fullWidth>
                            <Typography gutterBottom>
                                Room <b>{info.id}</b> is protected. Enter the access passphrase to
                                join.
                            </Typography>
                            <TextField
                                fullWidth
                                autoFocus
                                type="password"
                                value={password}
                                onChange={(e) => setPassword(e.target.value)}
                                label="Access passphrase"
                                margin="dense"
                                error={info.rejected}
                                helperText={
                                    info.rejected ? 'Incorrect passphrase, try again.' : undefined
                                }
                            />
                            <Button
                                type="submit"
                                fullWidth
                                variant="contained"
                                sx={{marginTop: 1}}
                            >
                                Join Room
                            </Button>
                        </FormControl>
                    </form>
                </Paper>
            </Grid>
            <div style={{position: 'absolute', margin: '0 auto', bottom: 0}}>
                <Link href="https://github.com/screego/server/">GitHub</Link>
            </div>
        </Grid>
    );
};
